package hostgit

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ErrTreeTooLarge means the commit's tree holds more files, or more bytes, than the
// host will check out. A few kilobytes of tree objects can name billions of paths
// (a tree that lists the same subtree twice, nested), and only the compressed size
// of a bundle is capped, so the expanded size is checked before anything is written
// to disk (prepare, editor copy).
var ErrTreeTooLarge = errors.New("the tree is larger than the host will check out")

// The most a checkout of an agent's commit may hold.
const (
	MaxTreeEntries = 200_000
	MaxTreeBytes   = 2 << 30
	// MaxTopicCommits bounds the commits a prepare replays: each one is checked out
	// by the rebase, so each one's tree is checked.
	MaxTopicCommits = 2_000
	// MaxTopicEntries and MaxTopicBytes bound what a whole topic adds: the entries
	// (trees count) and blob bytes that its commits introduce over their parents,
	// summed over all commits. See CheckCommits for why this bounds an autosquash.
	MaxTopicEntries = 400_000
	MaxTopicBytes   = 4 << 30
)

// The topic limits as the checks read them; tests lower them.
var (
	topicEntryLimit int64 = MaxTopicEntries
	topicByteLimit  int64 = MaxTopicBytes
)

// Hard deadlines, so a hostile object graph cannot hold the supervisor: the whole
// check of a topic, the whole prepare, and the whole editor copy.
const (
	CheckTimeout      = 2 * time.Minute
	PrepareTimeout    = 15 * time.Minute
	EditorCopyTimeout = 10 * time.Minute
)

// ErrTooManyCommits means the topic holds more new commits than the host will replay.
var ErrTooManyCommits = errors.New("the topic has more commits than the host will replay")

// CheckTree lists the tree of ref the way a checkout would see it, streaming and
// counting (trees count too, so a bomb without a single file is caught), and stops at
// the first limit: more than MaxTreeEntries entries or more than MaxTreeBytes of blobs. It never holds the listing in memory and kills git as soon
// as a limit is passed, so a nested-tree bomb costs a bounded amount of work.
func (r *Repo) CheckTree(ctx context.Context, ref string) error {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	return r.checkTree(ctx, ref, MaxTreeEntries, MaxTreeBytes)
}

// CheckCommits checks the tree of every commit in exclude..tip (all of tip's history
// when exclude is empty), because a rebase writes each of them to the host's worktree:
// a bomb in one commit that a later commit deletes would leave the tip small. A tree
// already checked (by object id) is not checked again, and the number of commits and
// the total time are bounded. tip and exclude are commit IDs.
//
// Each tree alone can be small while the rebase still builds a bigger one:
// --autosquash moves every "fixup!" commit up to its target, so the target's tree
// becomes the union of what many commits added, even though later commits deleted it
// again. So the topic's additions are capped as a whole: the entries and blob bytes
// each (non-merge) commit introduces over its parent, summed, each distinct new
// object once. A rebase only applies those changes (a conflict aborts it), so no tree
// it builds can hold more than the target's own tree (bounded by MaxTreeEntries) plus
// that sum. Counting what a commit adds, not its whole tree, keeps a topic of many
// small commits in a large repository well under the cap, while a fixup chain
// that adds 100 files and drops them again, 1,000 times, is refused.
func (r *Repo) CheckCommits(ctx context.Context, tip, exclude string) error {
	return r.checkCommits(ctx, tip, exclude, topicEntryLimit, topicByteLimit)
}

func (r *Repo) checkCommits(ctx context.Context, tip, exclude string, maxEntries, maxBytes int64) error {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	revs := []string{"--end-of-options", tip}
	if exclude != "" {
		revs = append(revs, "^"+exclude)
	}
	// --max-count bounds the listing before it is read; one more than the cap shows
	// that the cap was passed.
	out, err := r.g.run(ctx, r.path, false, nil, append([]string{"log", "--format=%T", "--max-count=" + strconv.Itoa(MaxTopicCommits+1)}, revs...)...)
	if err != nil {
		return err
	}
	trees := strings.Fields(string(out))
	if len(trees) > MaxTopicCommits {
		return fmt.Errorf("%w: more than %d commits", ErrTooManyCommits, MaxTopicCommits)
	}
	seen := make(map[string]bool, len(trees))
	for _, tree := range trees {
		if seen[tree] {
			continue
		}
		seen[tree] = true
		if err := r.checkTree(ctx, tree, MaxTreeEntries, MaxTreeBytes); err != nil {
			return err
		}
	}
	return r.checkAdditions(ctx, revs, maxEntries, maxBytes)
}

// checkAdditions streams the raw diff of every non-merge commit against its parent
// and sums the entries it adds or changes (deletions add nothing), then the sizes of
// the new blobs, each counted once per occurrence.
func (r *Repo) checkAdditions(ctx context.Context, revs []string, maxEntries, maxBytes int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := append([]string{"log", "--no-merges", "--root", "--format=", "--raw", "-r", "-t", "--no-renames", "--no-abbrev"}, revs...)
	cmd := r.g.command(ctx, r.path, false, nil, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Deletions add nothing, but listing them still costs: the excluded side is the
	// target's own tree (trusted) and the included side is checked above, so their
	// count is bounded by MaxTreeEntries per commit; this stops a pathological one.
	var entries, lines int64
	blobs := map[string]int64{} // new blob id -> how many times the topic adds it
	over := false
	br := bufio.NewReaderSize(pipe, 1<<16)
	for {
		line, rerr := br.ReadString('\n')
		// ":<old mode> <new mode> <old id> <new id> <status>\t<path>"
		if meta, _, ok := strings.Cut(line, "\t"); ok && strings.HasPrefix(meta, ":") {
			f := strings.Fields(meta[1:])
			if lines++; lines > maxEntries+MaxTreeEntries {
				over = true
				break
			}
			if len(f) == 5 && f[4] != "D" {
				entries++
				if f[1] != "040000" && f[1] != "160000" {
					blobs[f[3]]++
				}
				if entries > maxEntries {
					over = true
					break
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	if over {
		cancel()
		_ = cmd.Wait()
		return fmt.Errorf("%w: the topic adds more than %d entries", ErrTreeTooLarge, maxEntries)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: checking the topic ran out of time", ErrTreeTooLarge)
		}
		return fmt.Errorf("git log: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if len(blobs) == 0 {
		return nil
	}
	// Count per occurrence, like checkTree's per-path rule: a bundle stores a blob
	// once, but the autosquash writes a copy at every path that re-adds it.
	var in strings.Builder
	for id := range blobs {
		in.WriteString(id + "\n")
	}
	sc := r.g.command(ctx, r.path, false, nil, "cat-file", "--batch-check=%(objectname) %(objectsize)")
	sc.Stdin = strings.NewReader(in.String())
	var serr bytes.Buffer
	sc.Stderr = &serr
	sizes, err := sc.Output()
	if err != nil {
		return fmt.Errorf("git cat-file: %w: %s", err, strings.TrimSpace(serr.String()))
	}
	total := blobBytes(string(sizes), blobs, maxBytes)
	if total > maxBytes {
		return fmt.Errorf("%w: the topic adds more than %d bytes", ErrTreeTooLarge, maxBytes)
	}
	return nil
}

// blobBytes sums the sizes that `cat-file --batch-check=%(objectname) %(objectsize)`
// printed, each weighted by its occurrences in blobs. Sizes are matched by object name,
// not by position: a missing object prints "<name> missing" and must not shift the rest.
// It returns more than maxBytes as soon as the total passes it.
func blobBytes(out string, blobs map[string]int64, maxBytes int64) int64 {
	var total int64
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		n, perr := strconv.ParseInt(f[1], 10, 64)
		count, ok := blobs[f[0]]
		if perr != nil || !ok {
			continue
		}
		if n > 0 && count > (maxBytes-total)/n {
			return maxBytes + 1
		}
		total += n * count
	}
	return total
}

func (r *Repo) checkTree(ctx context.Context, ref string, maxEntries, maxBytes int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := r.g.command(ctx, r.path, false, nil, "ls-tree", "-r", "-t", "-l", "-z", "--full-tree", "--end-of-options", ref+"^{tree}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var entries, size int64
	over := false
	br := bufio.NewReaderSize(out, 1<<16)
	for {
		rec, rerr := br.ReadBytes(0)
		if len(rec) > 0 {
			entries++
			// "<mode> <type> <object> <size>\t<path>": the size is "-" for a submodule
			meta, _, _ := bytes.Cut(rec, []byte{'\t'})
			if f := strings.Fields(string(meta)); len(f) == 4 {
				if n, perr := strconv.ParseInt(f[3], 10, 64); perr == nil {
					size += n
				}
			}
			if entries > maxEntries || size > maxBytes {
				over = true
				break
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				over = false
			}
			break
		}
	}
	if over {
		cancel() // stop git: the listing would only grow
		_ = cmd.Wait()
		return fmt.Errorf("%w: more than %d entries or %d bytes in %s", ErrTreeTooLarge, maxEntries, maxBytes, ref)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: checking %s ran out of time", ErrTreeTooLarge, ref)
		}
		return fmt.Errorf("git ls-tree %s: %w: %s", ref, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
