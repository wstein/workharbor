package hostgit

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Repo is a bare repository the supervisor owns. Cleanup (rebase, fold,
// signing) and push run here, never in an agent's checkout.
type Repo struct {
	g    *Git
	path string

	// the topic limits CheckCommits applies; zero means MaxTopicEntries and
	// MaxTopicBytes. Only a test sets them, on its own Repo.
	entryLimit, byteLimit int64
}

// InitBare creates a bare repository at path, which must not exist yet and
// whose parent must.
func (g *Git) InitBare(ctx context.Context, path string) (*Repo, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: %q must be absolute", ErrBadPath, path)
	}
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

// Run runs a git command in this repository, under the hardened floor and with
// no transport but the local one disabled. The caller adds the options of the
// command (for example the remote and refspec of a push).
func (r *Repo) Run(ctx context.Context, args ...string) ([]byte, error) {
	return r.g.run(ctx, r.path, false, nil, args...)
}

// RunCapped is Run with at most limit bytes of standard output. Output over the
// cap is ErrOutputTooLarge, never a truncation; use it wherever the output
// depends on repository content an agent could have written.
func (r *Repo) RunCapped(ctx context.Context, limit int64, args ...string) ([]byte, error) {
	return r.g.runCapped(ctx, r.path, limit, args...)
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
// ValidBranch reports whether name is a branch name hostgit accepts.
func ValidBranch(name string) bool { return validBranch(name) }

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
