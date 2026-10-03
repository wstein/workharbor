package hostgit

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var hexIDRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// limitWriter fails with ErrBundleTooLarge on the byte after limit.
type limitWriter struct {
	w     io.Writer
	left  int64
	limit int64
	err   error // the first failure, which git's own broken pipe would hide
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	if int64(len(p)) > l.left {
		l.err = fmt.Errorf("%w (%d bytes)", ErrBundleTooLarge, l.limit)
		return 0, l.err
	}
	n, err := l.w.Write(p)
	l.left -= int64(n)
	if err != nil {
		l.err = err
	}
	return n, err
}

// StreamBundle writes a git bundle of the commits after base up to tip to w,
// at most maxBytes (design §4.5, D51): the prepared commits, for a check in an
// environment that holds base. Both are full commit IDs of this repository.
// git runs here, in the supervisor's own copy, never in a workspace. The bundle
// needs a ref, so one is made for the stream and removed on every path.
func (r *Repo) StreamBundle(ctx context.Context, w io.Writer, base, tip string, maxBytes int64) error {
	if !hexIDRe.MatchString(base) || !hexIDRe.MatchString(tip) {
		return fmt.Errorf("%w: a bundle is made between full commit IDs", ErrBadPath)
	}
	if maxBytes <= 0 {
		return fmt.Errorf("%w: a limit is needed", ErrBadPath)
	}
	ref := "refs/whr/bundle/" + tip
	if _, err := r.Run(ctx, "update-ref", ref, tip); err != nil {
		return err
	}
	defer func() { _, _ = r.Run(context.WithoutCancel(ctx), "update-ref", "-d", ref) }()
	cmd := r.g.command(ctx, r.path, false, nil, "bundle", "create", "-", "^"+base, ref)
	var stderr bytes.Buffer
	lw := &limitWriter{w: w, left: maxBytes, limit: maxBytes}
	cmd.Stdout, cmd.Stderr = lw, &stderr
	if err := cmd.Run(); err != nil {
		if lw.err != nil {
			return lw.err
		}
		return fmt.Errorf("git bundle create: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
