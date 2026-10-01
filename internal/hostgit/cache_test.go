package hostgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forgeRepo is a stand-in for the forge: a plain repository with n commits on
// main, built with plain git (the forge is not hostile; the checkouts are).
type forgeRepo struct {
	t    *testing.T
	dir  string
	env  []string
	next int
}

func newForge(t *testing.T, base string, commits int) *forgeRepo {
	t.Helper()
	f := &forgeRepo{t: t, dir: filepath.Join(base, "forge"), env: plainEnv(filepath.Join(base, "forge-home"))}
	for _, d := range []string{f.dir, filepath.Join(base, "forge-home")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(t, f.env, f.dir, "init", "--quiet", "-b", "main")
	f.commit(commits)
	return f
}

// commit adds n commits to main.
func (f *forgeRepo) commit(n int) {
	f.t.Helper()
	for range n {
		f.next++
		name := filepath.Join(f.dir, "file.txt")
		if err := os.WriteFile(name, []byte(strconv.Itoa(f.next)+"\n"), 0o600); err != nil {
			f.t.Fatal(err)
		}
		mustGit(f.t, f.env, f.dir, "add", "file.txt")
		mustGit(f.t, f.env, f.dir, "commit", "--quiet", "-m", "c"+strconv.Itoa(f.next))
	}
}

func (f *forgeRepo) count(dir, ref string) int {
	f.t.Helper()
	n, err := strconv.Atoi(mustGit(f.t, f.env, dir, "rev-list", "--count", ref))
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func openCache(t *testing.T, g *Git, base string, f *forgeRepo, depth int) *Cache {
	t.Helper()
	c, err := g.OpenCache(context.Background(), filepath.Join(base, "cache.git"), CacheConfig{Source: f.dir, CloneDepth: depth})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSharedTopicBorrowsFromTheCache(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 5)
	c := openCache(t, g, base, f, 0)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if shallow, _ := c.IsShallow(ctx); shallow {
		t.Error("a full-depth cache must not be shallow")
	}

	dest := filepath.Join(base, "ws", "t1")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatal(err)
	}
	topic, err := c.CloneTopic(ctx, dest, "main", "agent/topic")
	if err != nil {
		t.Fatal(err)
	}
	if len(topic.Alternates) != 1 || topic.Alternates[0] != c.ObjectsDir() || topic.Branch != "agent/topic" {
		t.Errorf("topic = %+v, want it to borrow from the cache's objects directory", topic)
	}
	env := plainEnv(filepath.Join(base, "home"))
	if got := mustGit(t, env, dest, "rev-parse", "--abbrev-ref", "HEAD"); got != "agent/topic" {
		t.Errorf("HEAD = %q", got)
	}
	if got := f.count(dest, "HEAD"); got != 5 {
		t.Errorf("%d commits in the topic, want the full history", got)
	}
	// The objects stay in the cache: the clone's own store holds none of them.
	if loose := mustGit(t, env, dest, "count-objects"); !strings.HasPrefix(loose, "0 objects") {
		t.Errorf("the shared clone copied objects: %q", loose)
	}
	// An empty template: no hooks were copied from anywhere.
	if entries, _ := os.ReadDir(filepath.Join(dest, ".git", "hooks")); len(entries) != 0 {
		t.Errorf("the clone has %d hooks", len(entries))
	}

	// hostgit accepts the checkout when the cache is the one listed alternate,
	// and refuses it when no cache is listed.
	listed := newGit(t, WithAlternates(c.ObjectsDir()))
	if _, err := listed.Untrusted(dest); err != nil {
		t.Errorf("a checkout that borrows from the listed cache was refused: %v", err)
	}
	if _, err := g.Untrusted(dest); !errors.Is(err, ErrAlternates) {
		t.Errorf("a checkout that borrows from an unlisted cache = %v, want ErrAlternates", err)
	}
}

func TestShallowTopicIsSelfContained(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 10)
	c := openCache(t, g, base, f, 3)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if shallow, _ := c.IsShallow(ctx); !shallow {
		t.Fatal("a cache with clone_depth 3 must be shallow")
	}
	if got := f.count(c.Path(), "main"); got != 3 {
		t.Errorf("the cache has %d commits, want 3", got)
	}

	dest := filepath.Join(base, "t1")
	topic, err := c.CloneTopic(ctx, dest, "main", "agent/topic")
	if err != nil {
		t.Fatal(err)
	}
	if len(topic.Alternates) != 0 {
		t.Errorf("a shallow topic borrows %v; --shared is ignored for a shallow source and nothing may be mounted", topic.Alternates)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git", "objects", "info", "alternates")); err == nil {
		t.Error("the shallow clone has an alternates file")
	}
	// Self-contained: it works with the cache gone.
	if err := os.RemoveAll(c.Path()); err != nil {
		t.Fatal(err)
	}
	if got := f.count(dest, "HEAD"); got != 3 {
		t.Errorf("%d commits in the shallow topic, want 3", got)
	}
}

func TestAShallowCacheWithDepthZeroIsRefusedNotCopied(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 6)
	c := openCache(t, g, base, f, 2)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	// The setting was changed to 0 but the cache has not been refreshed yet.
	full, err := g.OpenCache(ctx, c.Path(), CacheConfig{Source: f.dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := full.CloneTopic(ctx, filepath.Join(base, "t1"), "main", "agent/topic"); !errors.Is(err, ErrShallowCache) {
		t.Fatalf("CloneTopic = %v, want ErrShallowCache", err)
	}
	// A refresh makes the cache complete again.
	if err := full.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if shallow, _ := full.IsShallow(ctx); shallow || f.count(full.Path(), "main") != 6 {
		t.Errorf("the refresh must unshallow the cache: shallow %v, %d commits", shallow, f.count(full.Path(), "main"))
	}
	if _, err := full.CloneTopic(ctx, filepath.Join(base, "t1"), "main", "agent/topic"); err != nil {
		t.Errorf("after the refresh: %v", err)
	}
}

// A shallow cache cannot fast-forward, so a plain fetch is rejected; the
// forced refspec keeps refreshing it, even after the forge rewrote history.
func TestRefreshOfAShallowCacheFollowsTheForge(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 8)
	c := openCache(t, g, base, f, 2)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	f.commit(3)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatalf("after the forge moved: %v", err)
	}
	want := mustGit(t, f.env, f.dir, "rev-parse", "main")
	if got := mustGit(t, plainEnv(base), c.Path(), "rev-parse", "main"); got != want {
		t.Errorf("the cache is at %s, the forge at %s", got, want)
	}
	// The forge rewrites its last commit (a forced push).
	mustGit(t, f.env, f.dir, "commit", "--quiet", "--amend", "-m", "rewritten")
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatalf("after a forced update: %v", err)
	}
	want = mustGit(t, f.env, f.dir, "rev-parse", "main")
	if got := mustGit(t, plainEnv(base), c.Path(), "rev-parse", "main"); got != want {
		t.Errorf("the cache did not follow the rewrite: %s, want %s", got, want)
	}
}

func TestSourcesAndNamesAreValidated(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	for _, src := range []string{
		"", "relative/dir", "-uhttps://x", "--upload-pack=touch /tmp/x", "ssh://git@host/repo", "git@host:repo.git",
		"ext::sh -c touch% /tmp/x", "file:///tmp/repo", "http://insecure.example/r.git", "https://user:secret@example.test/r.git",
		"https://example.test/r.git?x=1", "/does/not/exist", "https://", "/tmp/with space",
	} {
		if _, err := g.OpenCache(ctx, filepath.Join(base, "c.git"), CacheConfig{Source: src}); !errors.Is(err, ErrBadSource) {
			t.Errorf("OpenCache(%q) = %v, want ErrBadSource", src, err)
		}
	}
	if _, err := g.OpenCache(ctx, filepath.Join(base, "c.git"), CacheConfig{Source: base, CloneDepth: -1}); !errors.Is(err, ErrBadSource) {
		t.Errorf("a negative clone_depth = %v", err)
	}
	if _, err := g.OpenCache(ctx, filepath.Join(base, "c.git"), CacheConfig{Source: base}); err != nil {
		t.Fatalf("a good source: %v", err)
	}

	f := newForge(t, t.TempDir(), 2)
	c := openCache(t, g, t.TempDir(), f, 0)
	for _, b := range []string{"", "-x", "a..b", "main.lock", "/main"} {
		if err := c.Refresh(ctx, b); !errors.Is(err, ErrBadBranch) {
			t.Errorf("Refresh(%q) = %v, want ErrBadBranch", b, err)
		}
	}
	dest := filepath.Join(t.TempDir(), "t1")
	if _, err := c.CloneTopic(ctx, dest, "main", "--upload-pack=x"); !errors.Is(err, ErrBadBranch) {
		t.Errorf("a topic named like an option = %v", err)
	}
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CloneTopic(ctx, filepath.Join(t.TempDir(), "x", "y", "t1"), "main", "agent/t"); !errors.Is(err, ErrBadPath) {
		t.Errorf("a destination whose parent is missing = %v", err)
	}
}

// topicWithCommit clones a topic and makes one commit in it, as an agent would.
func topicWithCommit(t *testing.T, c *Cache, base, name string) string {
	t.Helper()
	dest := filepath.Join(base, name)
	if _, err := c.CloneTopic(context.Background(), dest, "main", "agent/topic"); err != nil {
		t.Fatal(err)
	}
	env := plainEnv(filepath.Join(base, "home"))
	if err := os.WriteFile(filepath.Join(dest, "agent.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, env, dest, "add", "agent.txt")
	mustGit(t, env, dest, "commit", "--quiet", "-m", "agent work")
	return dest
}

// With a depth, the target can move further than the depth past the topic's
// fork point: then there is no merge base until the histories are deepened.
func TestEnsureMergeBaseDeepensUntilThereIsOne(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 6)
	c := openCache(t, g, base, f, 2)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	dest := topicWithCommit(t, c, base, "t1")
	forkPoint := mustGit(t, plainEnv(base), dest, "rev-parse", "HEAD~1")

	// The target moves 10 commits on: far beyond the depth of 2.
	f.commit(10)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}

	r, err := g.InitBare(ctx, filepath.Join(base, "supervisor.git"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FetchBranch(ctx, dest, "agent/topic"); err != nil {
		t.Fatal(err)
	}
	if err := r.FetchTarget(ctx, c, "main", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := r.MergeBase(ctx, "refs/heads/main", "refs/heads/agent/topic"); ok {
		t.Fatal("setup: the shallow histories must have no merge base yet")
	}

	got, err := c.EnsureMergeBase(ctx, r, "main", "agent/topic", DeepenOptions{Step: 3, Max: 30})
	if err != nil {
		t.Fatal(err)
	}
	if got != forkPoint {
		t.Errorf("merge base %s, want the fork point %s", got, forkPoint)
	}
}

func TestEnsureMergeBaseGivesUpAtTheLimit(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 6)
	c := openCache(t, g, base, f, 2)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	dest := topicWithCommit(t, c, base, "t1")
	f.commit(30)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	r, _ := g.InitBare(ctx, filepath.Join(base, "supervisor.git"))
	if _, err := r.FetchBranch(ctx, dest, "agent/topic"); err != nil {
		t.Fatal(err)
	}
	if err := r.FetchTarget(ctx, c, "main", 0); err != nil {
		t.Fatal(err)
	}
	_, err := c.EnsureMergeBase(ctx, r, "main", "agent/topic", DeepenOptions{Step: 2, Max: 4})
	if !errors.Is(err, ErrNoMergeBase) {
		t.Errorf("EnsureMergeBase = %v, want ErrNoMergeBase so that the caller opens a Decision", err)
	}
}

func TestEnsureMergeBaseOnFullHistoryNeedsNoDeepening(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 5)
	c := openCache(t, g, base, f, 0)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	dest := topicWithCommit(t, c, base, "t1")
	f.commit(4)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	r, _ := g.InitBare(ctx, filepath.Join(base, "supervisor.git"))
	// The topic borrows objects from the cache, so the supervisor's copy fetches
	// them from the cache too; the checkout is read through the listed alternate.
	listed := newGit(t, WithAlternates(c.ObjectsDir()))
	r2, err := listed.OpenBare(ctx, r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r2.FetchBranch(ctx, dest, "agent/topic"); err != nil {
		t.Fatal(err)
	}
	if err := r2.FetchTarget(ctx, c, "main", 0); err != nil {
		t.Fatal(err)
	}
	got, err := c.EnsureMergeBase(ctx, r2, "main", "agent/topic", DeepenOptions{})
	if err != nil || got == "" {
		t.Errorf("EnsureMergeBase = %q, %v", got, err)
	}
	// Histories that are complete and unrelated have no base to find, and no
	// deepening to try.
	mustGit(t, plainEnv(base), r2.Path(), "update-ref", "refs/heads/orphan", mustGit(t, plainEnv(base), r2.Path(), "commit-tree", "-m", "orphan", "refs/heads/main^{tree}"))
	if _, err := c.EnsureMergeBase(ctx, r2, "main", "orphan", DeepenOptions{}); !errors.Is(err, ErrNoMergeBase) {
		t.Errorf("unrelated complete histories = %v, want ErrNoMergeBase", err)
	}
}
