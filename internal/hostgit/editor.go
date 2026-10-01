package hostgit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Errors of the editor copy.
var (
	ErrInsideWorkspace = errors.New("the editor copy must not be inside the workspace root")
	ErrNotACopy        = errors.New("the directory is not a supervisor-owned editor copy")
	ErrCopyDiverged    = errors.New("the editor copy has changes that do not fast-forward")
)

// autoRunFiles are files in a repository that an editor or a shell hook acts
// on by itself when the folder is opened.
var autoRunFiles = []string{
	".vscode/tasks.json", ".vscode/settings.json", ".vscode/launch.json", ".envrc",
	".devcontainer", ".devcontainer.json", ".idea", ".husky", ".githooks", ".gitattributes", ".gitmodules",
}

// EditorCopy gives the developer's editor a copy of a topic that the agent
// never touched (design §4.5, threat model T15). It clones this supervisor-owned
// repository, which FetchBranch filled from the stopped environment, into dest:
// an empty template, no hooks, none of the agent's config, no alternates. A
// later call refreshes the copy with a fast-forward only, so the developer's
// edits are never overwritten (ErrCopyDiverged). dest must lie outside the
// workspace root, so an agent's checkout cannot be handed out by mistake.
// It returns the files in the copy that an editor may act on by itself, for the
// UI to warn about before the folder is trusted.
func (r *Repo) EditorCopy(ctx context.Context, dest, branch string) ([]string, error) {
	if !validBranch(branch) {
		return nil, fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	if !filepath.IsAbs(dest) {
		return nil, fmt.Errorf("%w: %q", ErrBadPath, dest)
	}
	parent, err := resolveDir(filepath.Dir(dest))
	if err != nil {
		return nil, err
	}
	dest = filepath.Join(parent, filepath.Base(dest))
	if r.g.inWorkspace(dest) {
		return nil, fmt.Errorf("%w: %q", ErrInsideWorkspace, dest)
	}

	if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
		if _, err := r.g.run(ctx, parent, true, nil, "clone", "--quiet", "--no-hardlinks", "--no-tags", "--template=",
			"--branch", branch, "--", r.path, dest); err != nil {
			return nil, err
		}
	} else if err := r.refreshCopy(ctx, dest, branch); err != nil {
		return nil, err
	}
	return autoRun(dest), nil
}

// refreshCopy fast-forwards an existing copy to the branch, after checking
// that it is a copy of this repository.
func (r *Repo) refreshCopy(ctx context.Context, dest, branch string) error {
	out, err := r.g.run(ctx, dest, false, nil, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(string(out)) != r.path {
		return fmt.Errorf("%w: %q", ErrNotACopy, dest)
	}
	if _, err := r.g.run(ctx, dest, true, nil, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--force", "--",
		"origin", "+refs/heads/"+branch+":refs/remotes/origin/"+branch); err != nil {
		return err
	}
	if _, err := r.g.run(ctx, dest, false, nil, "merge", "--quiet", "--ff-only", "refs/remotes/origin/"+branch); err != nil {
		return fmt.Errorf("%w: %v", ErrCopyDiverged, err) //nolint:errorlint // the git output is the detail
	}
	return nil
}

// autoRun lists the files of the copy that an editor may act on by itself.
func autoRun(dir string) []string {
	var found []string
	for _, name := range autoRunFiles {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}
