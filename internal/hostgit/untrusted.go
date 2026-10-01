package hostgit

import (
	"context"
	"fmt"
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
	g    *Git
	path string
}

// Untrusted returns a handle on an agent's checkout.
func (g *Git) Untrusted(path string) (*Untrusted, error) {
	if err := checkDir(path); err != nil {
		return nil, err
	}
	return &Untrusted{g: g, path: path}, nil
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
	return u.g.run(ctx, u.path, false, args...)
}
