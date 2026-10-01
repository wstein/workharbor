package hostgit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SeedAgentClone creates the agent clone of a workspace (design D42, §4.5):
// a full, independent clone of source at dest, on the integration branch. The
// source is an absolute path or an https URL, as for a cache. The clone borrows
// nothing from it (no alternates, no hardlinks, no template), so the source's
// .git is never mounted into an environment and nothing in it is shared. The
// origin points at originURL, which carries no credentials, never at a host
// path.
//
// This is the only time the host runs git in a workspace's clone: the clone
// does not exist yet, so nothing planted can be in it. After the agent starts,
// the host never runs git there; commits leave as bundles (§4.5).
func (g *Git) SeedAgentClone(ctx context.Context, dest, source, integration, originURL string) error {
	if err := validSource(source); err != nil {
		return err
	}
	if !validBranch(integration) {
		return fmt.Errorf("%w: %q", ErrBadBranch, integration)
	}
	if err := validSource(originURL); err != nil || !strings.HasPrefix(originURL, "https://") {
		return fmt.Errorf("%w: origin %q must be a plain https URL", ErrBadSource, originURL)
	}
	if !filepath.IsAbs(dest) || filepath.Clean(dest) != dest {
		return fmt.Errorf("%w: %q", ErrBadPath, dest)
	}
	parent := filepath.Dir(dest)
	if err := checkDir(parent); err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%w: %q already exists", ErrBadPath, dest)
	}

	env, pre := netArgs(source)
	args := append(pre, "clone", "--quiet", "--no-tags", "--no-local", "--template=", "--branch", integration, "--", source, dest)
	if _, err := g.run(ctx, parent, true, env, args...); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	if _, err := g.run(ctx, dest, false, nil, "remote", "set-url", "origin", originURL); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	if _, err := os.Lstat(filepath.Join(dest, ".git", "objects", "info", "alternates")); err == nil {
		_ = os.RemoveAll(dest)
		return fmt.Errorf("%w: the clone borrows objects from another repository", ErrBadPath)
	}
	return nil
}
