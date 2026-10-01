package hostgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// The agent's hook, file-system monitor, ssh command, pager, filters and
// pack-objects hook are planted; fetching the branch must run none of them
// and must copy none of them.
func TestFetchBranchRunsAndCopiesNothingPlanted(t *testing.T) {
	g := newGit(t)
	p := newPlant(t)
	r := newSupervised(t, g)
	ctx := context.Background()

	sha, err := r.FetchBranch(ctx, p.repo, "agent/topic")
	if err != nil {
		t.Fatal(err)
	}
	if sha != p.sha {
		t.Errorf("fetched %s, want the agent's commit %s", sha, p.sha)
	}
	if got := p.fired(); len(got) != 0 {
		t.Errorf("fetching ran %v on the host", got)
	}

	refs, err := r.Run(ctx, "for-each-ref", "--format=%(refname)")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(refs)); got != "refs/heads/agent/topic" {
		t.Errorf("refs in the supervised repository = %q, want only the fetched branch", got)
	}

	config, err := os.ReadFile(filepath.Join(r.Path(), "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"fsmonitor", "sshcommand", "pager", "filter", "packobjectshook", "hookspath", "credential", "editor"} {
		if strings.Contains(strings.ToLower(string(config)), key) {
			t.Errorf("the agent's %q setting reached the supervised repository's config", key)
		}
	}
	if _, err := r.Run(ctx, "fsck", "--strict"); err != nil {
		t.Errorf("the fetched objects do not check out: %v", err)
	}
}

func TestFetchBranchTakesNewCommitsAndRewrites(t *testing.T) {
	g := newGit(t)
	p := newPlant(t)
	r := newSupervised(t, g)
	ctx := context.Background()
	first, err := r.FetchBranch(ctx, p.repo, "agent/topic")
	if err != nil {
		t.Fatal(err)
	}

	env := plainEnv(t.TempDir())
	mustGit(t, env, p.repo, "-c", "core.hooksPath="+os.DevNull, "-c", "core.fsmonitor=false", "commit", "--allow-empty", "--quiet", "-m", "more work")
	second := mustGit(t, env, p.repo, "rev-parse", "HEAD")
	if second == first {
		t.Fatal("setup: the agent made no new commit")
	}
	p.clearCanary(t) // the test's own plain git above may have fired a plant
	got, err := r.FetchBranch(ctx, p.repo, "agent/topic")
	if err != nil || got != second {
		t.Fatalf("second fetch = %q, %v; want %s", got, err, second)
	}

	// The agent rewrites its branch; the copy follows, because the supervisor
	// treats the agent's branch as input, not as history to protect.
	mustGit(t, env, p.repo, "-c", "core.hooksPath="+os.DevNull, "-c", "core.fsmonitor=false", "reset", "--quiet", "--hard", first)
	p.clearCanary(t)
	if got, err := r.FetchBranch(ctx, p.repo, "agent/topic"); err != nil || got != first {
		t.Fatalf("fetch after a rewrite = %q, %v; want %s", got, err, first)
	}
	if got := p.fired(); len(got) != 0 {
		t.Errorf("fetching ran %v on the host", got)
	}
}

func TestFetchBranchRefusesBadInput(t *testing.T) {
	g := newGit(t)
	p := newPlant(t)
	r := newSupervised(t, g)
	ctx := context.Background()

	for _, branch := range []string{
		"", "-x", "--upload-pack=sh", "../x", "a..b", "a b", "a;b", "a/", "/a", "a.lock", ".hidden", "a/.b", "a//b", "a\nb", "a:b", "a~1", "a^",
	} {
		if _, err := r.FetchBranch(ctx, p.repo, branch); !errors.Is(err, ErrBadBranch) {
			t.Errorf("FetchBranch(%q) = %v, want ErrBadBranch", branch, err)
		}
	}
	for _, branch := range []string{"agent/topic", "agent/fix-1.2", "main", "a/b/c_d"} {
		if !validBranch(branch) {
			t.Errorf("validBranch(%q) = false", branch)
		}
	}
	for _, path := range []string{"", "relative/checkout", filepath.Join(t.TempDir(), "missing")} {
		if _, err := r.FetchBranch(ctx, path, "agent/topic"); !errors.Is(err, ErrBadPath) {
			t.Errorf("FetchBranch from %q = %v, want ErrBadPath", path, err)
		}
	}
	if _, err := r.FetchBranch(ctx, p.repo, "agent/missing"); err == nil {
		t.Error("fetching a branch the agent does not have must fail")
	}
	if got := p.fired(); len(got) != 0 {
		t.Errorf("refused fetches ran %v", got)
	}
}

// Cleanup and push work only on the supervisor-owned copy: a Repo is bare and
// supervisor-made, and the agent's own checkout cannot become one.
func TestOnlyASupervisorOwnedBareRepositoryCanBeOpened(t *testing.T) {
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
	g := newGit(t)
	if _, err := g.InitBare(context.Background(), filepath.Join(t.TempDir(), "no", "such", "parent.git")); !errors.Is(err, ErrBadPath) {
		t.Errorf("InitBare below a missing parent = %v, want ErrBadPath", err)
	}
	if _, err := g.InitBare(context.Background(), "relative.git"); err == nil {
		t.Error("InitBare with a relative path must fail")
	}
}
