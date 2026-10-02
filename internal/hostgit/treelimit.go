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
)

// CheckTree lists the tree of ref the way a checkout would see it, streaming and
// counting, and stops at the first limit: more than MaxTreeEntries files or more than
// MaxTreeBytes of blobs. It never holds the listing in memory and kills git as soon
// as a limit is passed, so a nested-tree bomb costs a bounded amount of work.
func (r *Repo) CheckTree(ctx context.Context, ref string) error {
	return r.checkTree(ctx, ref, MaxTreeEntries, MaxTreeBytes)
}

func (r *Repo) checkTree(ctx context.Context, ref string, maxEntries, maxBytes int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := r.g.command(ctx, r.path, false, nil, "ls-tree", "-r", "-l", "-z", "--full-tree", "--end-of-options", ref+"^{tree}")
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
		return fmt.Errorf("%w: more than %d files or %d bytes in %s", ErrTreeTooLarge, maxEntries, maxBytes, ref)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git ls-tree %s: %w: %s", ref, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
