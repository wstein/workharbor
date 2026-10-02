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
func (r *Repo) CheckCommits(ctx context.Context, tip, exclude string) error {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	args := []string{"log", "--format=%T", "--end-of-options", tip}
	if exclude != "" {
		args = append(args, "^"+exclude)
	}
	// "--end-of-options" must precede revisions only; the exclusion is a revision too.
	out, err := r.g.run(ctx, r.path, false, nil, args...)
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
	return nil
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
