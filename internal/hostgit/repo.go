package hostgit

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Repo is a bare repository the supervisor owns. Cleanup (rebase, fold,
// signing) and push run here, never in an agent's checkout.
type Repo struct {
	g    *Git
	path string
}

// InitBare creates a bare repository at path, which must not exist yet and
// whose parent must.
func (g *Git) InitBare(ctx context.Context, path string) (*Repo, error) {
	parent := dirOf(path)
	if err := checkDir(parent); err != nil {
		return nil, err
	}
	if _, err := g.run(ctx, parent, false, nil, "init", "--bare", "--quiet", "--", path); err != nil {
		return nil, err
	}
	return &Repo{g: g, path: path}, nil
}

// OpenBare opens an existing bare repository that the supervisor owns.
func (g *Git) OpenBare(ctx context.Context, path string) (*Repo, error) {
	if err := checkDir(path); err != nil {
		return nil, err
	}
	out, err := g.run(ctx, path, false, nil, "rev-parse", "--is-bare-repository")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(out)) != "true" {
		return nil, fmt.Errorf("%w: %q is not a bare repository", ErrBadPath, path)
	}
	return &Repo{g: g, path: path}, nil
}

// Path returns where the repository lives.
func (r *Repo) Path() string { return r.path }

// FetchBranch copies one branch from an agent's checkout into this
// repository under the same name and returns the commit it points to. It
// fetches no tags, no submodules and nothing else, checks every object, and
// uses only the file transport. Nothing of the agent's config, hooks or
// attributes is copied.
func (r *Repo) FetchBranch(ctx context.Context, agentCheckout, branch string) (string, error) {
	if !validBranch(branch) {
		return "", fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	_, gitDir, err := r.g.verifyCheckout(agentCheckout)
	if err != nil {
		return "", err
	}
	ref := "refs/heads/" + branch
	// The source is the verified .git directory, never the path as given.
	if _, err := r.g.run(ctx, r.path, true, nil,
		"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--update-shallow", "--force",
		"--", gitDir, "+"+ref+":"+ref); err != nil {
		return "", err
	}
	out, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Run runs a git command in this repository, under the hardened floor and with
// no transport but the local one disabled. The caller adds the options of the
// command (for example the remote and refspec of a push).
func (r *Repo) Run(ctx context.Context, args ...string) ([]byte, error) {
	return r.g.run(ctx, r.path, false, nil, args...)
}

func dirOf(path string) string {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "/"
	}
	return path[:i]
}

var branchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// validBranch accepts a plain branch name: no leading dash or slash, no
// "..", no empty or dotted path elements, no ".lock" suffix.
func validBranch(name string) bool {
	if !branchRe.MatchString(name) || strings.Contains(name, "..") || strings.HasSuffix(name, "/") ||
		strings.HasSuffix(name, ".lock") || strings.HasSuffix(name, ".") || strings.Contains(name, "//") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
