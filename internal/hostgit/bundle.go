package hostgit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Errors of importing a bundle.
var (
	// ErrBundleTooLarge means the stream was longer than the cap.
	ErrBundleTooLarge = errors.New("the bundle is larger than the limit")
	// ErrBundleBranch means the bundle does not carry the branch asked for.
	ErrBundleBranch = errors.New("the bundle does not contain the branch")
)

// ImportBundle imports one branch from a git bundle read from bundle, up to
// maxBytes, into this repository (design D42, §4.5). The bundle was made by git
// inside an environment, so it is untrusted data and nothing else: the host
// never runs git in the workspace it came from, and nothing of that .git (its
// config, hooks, attributes, filters) is read.
//
// The fetch is the check. `git bundle verify` passed a bundle truncated to half
// its size (spike #89); fetching it into a repository that lacks the new
// objects refused it and left no ref. So the bundle is first fetched into a
// scratch repository that borrows this repository's objects, which the bundle's
// prerequisites need, and holds nothing else; only a branch that arrived whole,
// with every object checked, is then fetched from the scratch into this
// repository. The scratch is removed on every path.
//
// The branch is fetched with force: this repository may hold an earlier
// import of the same branch. Whether a rewrite of already published commits is
// acceptable is the caller's rule (the revision's recorded source).
func (r *Repo) ImportBundle(ctx context.Context, branch string, bundle io.Reader, maxBytes int64) (string, error) {
	if !validBranch(branch) {
		return "", fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	if maxBytes <= 0 {
		return "", fmt.Errorf("%w: a limit is needed", ErrBadPath)
	}
	scratch, err := os.MkdirTemp("", "whr-bundle-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	file := filepath.Join(scratch, "in.bundle")
	if err := copyCapped(file, bundle, maxBytes); err != nil {
		return "", err
	}

	g := r.g
	sr, err := g.InitBare(ctx, filepath.Join(scratch, "scratch.git"))
	if err != nil {
		return "", err
	}
	// The bundle's prerequisites are commits this repository already has.
	if err := os.WriteFile(filepath.Join(sr.path, "objects", "info", "alternates"), []byte(filepath.Join(r.path, "objects")+"\n"), 0o600); err != nil {
		return "", err
	}
	ref := "refs/heads/" + branch
	if _, err := g.run(ctx, sr.path, true, nil,
		"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--force",
		"--", file, "+"+ref+":"+ref); err != nil {
		if strings.Contains(err.Error(), "couldn't find remote ref") {
			return "", fmt.Errorf("%w: %q", ErrBundleBranch, branch)
		}
		return "", fmt.Errorf("the bundle was refused: %w", err)
	}
	tip, err := sr.Run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("the bundle holds no commit for %s: %w", branch, err)
	}
	sha := strings.TrimSpace(string(tip))
	// Every object the branch needs must be complete before it is taken over.
	if _, err := sr.Run(ctx, "rev-list", "--objects", "--quiet", "--missing=error", sha); err != nil {
		return "", fmt.Errorf("the bundle is incomplete: %w", err)
	}

	if _, err := g.run(ctx, r.path, true, nil,
		"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--force",
		"--", sr.path, "+"+ref+":"+ref); err != nil {
		return "", err
	}
	out, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	if got := strings.TrimSpace(string(out)); got != sha {
		return "", fmt.Errorf("%w: imported %s, expected %s", ErrBadPath, got, sha)
	}
	return sha, nil
}

// copyCapped writes at most limit bytes of r to a new file at path, and fails
// with ErrBundleTooLarge if there is more.
func copyCapped(path string, r io.Reader, limit int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // a fresh file in a temporary directory just made
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("%w (%d bytes)", ErrBundleTooLarge, limit)
	}
	return nil
}
