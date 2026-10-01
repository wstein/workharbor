package hostgit

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// readOnlyPlumbing are the commands that may run in an agent's checkout. None
// of them updates the index, runs a hook, or applies a filter, textconv or
// file-system monitor.
var readOnlyPlumbing = map[string]bool{
	"rev-parse":    true,
	"rev-list":     true,
	"cat-file":     true,
	"for-each-ref": true,
	"ls-tree":      true,
	"merge-base":   true,
	"show-ref":     true,
}

// refusedFlags are options that would turn an allowed command into one that
// runs configured programs or leaves the repository.
var refusedFlags = []string{
	"--textconv", "--filters", "--ext-diff", "--exec-path",
	"--upload-pack", "--receive-pack", "--git-dir", "--work-tree", "--namespace",
}

// Untrusted is a checkout an agent can write. Only read-only plumbing runs in
// it; cleanup and push belong to a Repo the supervisor owns.
type Untrusted struct {
	g        *Git
	checkout string // the verified checkout
	gitDir   string // its verified .git directory
}

// Untrusted returns a handle on an agent's checkout after verifying it: under
// the workspace root, a real .git directory, no redirected object store and
// no alternates but the listed caches (see verifyCheckout).
func (g *Git) Untrusted(path string) (*Untrusted, error) {
	checkout, gitDir, err := g.verifyCheckout(path)
	if err != nil {
		return nil, err
	}
	return &Untrusted{g: g, checkout: checkout, gitDir: gitDir}, nil
}

// Run runs an allowed plumbing command and returns its output. The first
// argument must be the command itself, with no option before it.
func (u *Untrusted) Run(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 || !readOnlyPlumbing[args[0]] {
		name := "(none)"
		if len(args) > 0 {
			name = args[0]
		}
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, name)
	}
	for _, a := range args[1:] {
		for _, f := range refusedFlags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return nil, fmt.Errorf("%w: option %s", ErrNotAllowed, a)
			}
		}
	}
	// Git is pointed at the verified directory and never searches upward.
	env := []string{"GIT_DIR=" + u.gitDir, "GIT_CEILING_DIRECTORIES=" + filepath.Dir(u.checkout)}
	return u.g.run(ctx, u.checkout, false, env, args...)
}
