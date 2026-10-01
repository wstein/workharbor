package hostgit

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// options lists what one plumbing command accepts. None of the commands runs
// the index, a hook, a filter, a textconv driver or the file-system monitor.
// Options are matched exactly: git accepts any unambiguous prefix of a long
// option, so a list of refused options is not enough (`--textc` runs a
// textconv driver). Anything not listed is refused.
type options struct {
	flags  map[string]bool // exact options, such as "--verify"
	values []string        // options that carry a value, such as "--format="
}

func newOptions(flags []string, values ...string) options {
	o := options{flags: map[string]bool{}, values: values}
	for _, f := range flags {
		o.flags[f] = true
	}
	return o
}

func (o options) allows(arg string) bool {
	if o.flags[arg] {
		return true
	}
	for _, v := range o.values {
		if strings.HasPrefix(arg, v) {
			return true
		}
	}
	return false
}

// readOnlyPlumbing are the commands that may run in an agent's checkout and
// the options each accepts.
var readOnlyPlumbing = map[string]options{
	"rev-parse": newOptions([]string{
		"--verify", "-q", "--quiet", "--short", "--abbrev-ref", "--symbolic-full-name",
		"--is-bare-repository", "--show-toplevel", "--end-of-options",
	}, "--short=", "--abbrev-ref="),
	"rev-list": newOptions([]string{
		"--count", "--first-parent", "--reverse", "--no-merges", "--merges", "--parents",
		"--all", "--branches", "--end-of-options",
	}, "--max-count=", "--skip="),
	"cat-file": newOptions([]string{
		"-t", "-s", "-e", "-p", "--batch", "--batch-check", "--end-of-options",
	}, "--batch=", "--batch-check="),
	"for-each-ref": newOptions([]string{"--end-of-options"},
		"--format=", "--count=", "--sort="),
	"ls-tree": newOptions([]string{
		"-r", "-t", "-d", "-l", "--long", "--name-only", "--full-name", "--full-tree", "-z",
		"--end-of-options",
	}, "--abbrev="),
	"merge-base": newOptions([]string{
		"--is-ancestor", "--all", "--octopus", "--end-of-options",
	}),
	"show-ref": newOptions([]string{
		"--verify", "--head", "--heads", "--tags", "-q", "--quiet", "-s", "--hash",
		"-d", "--dereference", "--exists", "--end-of-options",
	}, "--hash=", "--abbrev="),
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
	name := "(none)"
	if len(args) > 0 {
		name = args[0]
	}
	allowed, ok := readOnlyPlumbing[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, name)
	}
	for _, a := range args[1:] {
		if a == "--" || a == "--end-of-options" {
			break // everything after is a path or a revision
		}
		if strings.HasPrefix(a, "-") && !allowed.allows(a) {
			return nil, fmt.Errorf("%w: option %s for %s", ErrNotAllowed, a, name)
		}
	}
	// Git is pointed at the verified directory and never searches upward.
	env := []string{"GIT_DIR=" + u.gitDir, "GIT_CEILING_DIRECTORIES=" + filepath.Dir(u.checkout)}
	return u.g.run(ctx, u.checkout, false, env, args...)
}
