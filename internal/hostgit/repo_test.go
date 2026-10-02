package hostgit

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newSupervised(t *testing.T, g *Git) *Repo {
	t.Helper()
	r, err := g.InitBare(context.Background(), filepath.Join(t.TempDir(), "supervised.git"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Cleanup and push work only on the supervisor-owned copy: a Repo is bare and
// supervisor-made, and the agent's own checkout cannot become one.
func TestOnlyASupervisorOwnedBareRepositoryCanBeOpened(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	p := newPlant(t)
	ctx := context.Background()

	if _, err := g.OpenBare(ctx, p.repo); !errors.Is(err, ErrBadPath) {
		t.Errorf("OpenBare(agent checkout) = %v, want ErrBadPath", err)
	}
	if got := p.fired(); len(got) != 0 {
		t.Errorf("OpenBare ran %v in the agent's checkout", got)
	}

	r := newSupervised(t, g)
	again, err := g.OpenBare(ctx, r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if again.Path() != r.Path() {
		t.Errorf("path = %q, want %q", again.Path(), r.Path())
	}
	if _, err := g.OpenBare(ctx, filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrBadPath) {
		t.Errorf("OpenBare(missing) = %v, want ErrBadPath", err)
	}
}

func TestSupervisedRepositoryHasNoTransportButFile(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	r := newSupervised(t, g)
	for _, remote := range []string{"https://example.invalid/x.git", "ssh://example.invalid/x.git", "git://example.invalid/x.git", "ext::sh -c id"} {
		out, err := r.Run(context.Background(), "ls-remote", remote)
		if err == nil {
			t.Errorf("ls-remote %s must be refused, got %q", remote, out)
		}
	}
}

func TestInitBareNeedsAnExistingParent(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	if _, err := g.InitBare(context.Background(), filepath.Join(t.TempDir(), "no", "such", "parent.git")); !errors.Is(err, ErrBadPath) {
		t.Errorf("InitBare below a missing parent = %v, want ErrBadPath", err)
	}
	if _, err := g.InitBare(context.Background(), "relative.git"); err == nil {
		t.Error("InitBare with a relative path must fail")
	}
}

// importBranch brings a branch of a plain test checkout into a supervisor
// repository, standing in for the bundle import of an agent's branch (the
// supervisor never reads a workspace's .git; a test's checkout is its own).
func importBranch(t *testing.T, r *Repo, checkout, branch string) {
	t.Helper()
	ref := "refs/heads/" + branch
	if _, err := r.g.run(context.Background(), r.path, true, nil,
		"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--update-shallow", "--force",
		"--", filepath.Join(checkout, ".git"), "+"+ref+":"+ref); err != nil {
		t.Fatal(err)
	}
}
