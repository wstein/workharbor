package hostgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Three parallel shallow fetches had failed 40 times in 60 on shallow.lock.
func TestParallelRefreshesAllSucceed(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 20)
	first := openCache(t, g, base, f, 3)
	if err := first.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Separate handles on one path, as separate tasks hold them.
			c, err := g.OpenCache(ctx, first.Path(), CacheConfig{Source: f.dir, CloneDepth: 3})
			if err != nil {
				errs <- err
				return
			}
			errs <- c.Refresh(ctx, "main")
			if i%3 == 0 {
				errs <- c.Deepen(ctx, "main", 2)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a parallel operation failed: %v", err)
		}
	}
}

func TestStaleGitLocksAreRecovered(t *testing.T) {
	ctx := context.Background()
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 5)
	c := openCache(t, g, base, f, 2)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	// A killed fetch leaves its lock files behind.
	stale := []string{
		filepath.Join(c.Path(), "shallow.lock"),
		filepath.Join(c.Path(), "packed-refs.lock"),
		filepath.Join(c.Path(), "refs", "heads", "main.lock"),
	}
	for _, p := range stale {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(c.Path(), "refs", "heads", "not-a-lock")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.commit(2)
	if err := c.Refresh(ctx, "main"); err != nil {
		t.Fatalf("a refresh after a killed one: %v", err)
	}
	for _, p := range stale {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("the stale lock %s was left", p)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("a file that is not a lock was removed")
	}
}

// The lock is waited for with the caller's context.
func TestTheCacheLockHonoursTheContext(t *testing.T) {
	g := newGit(t)
	base := t.TempDir()
	f := newForge(t, base, 2)
	c := openCache(t, g, base, f, 0)
	unlock, err := c.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := c.Refresh(ctx, "main"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a refresh behind a held lock = %v, want the context's deadline", err)
	}
	unlock()
	if err := c.Refresh(context.Background(), "main"); err != nil {
		t.Errorf("after the lock was released: %v", err)
	}
}

func TestRepositoryNamesMapToSafeDirectories(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := newGit(t, WithCacheRoot(root))
	a, err := g.CachePath("wstein/workharbor")
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(root, a); err != nil || strings.Contains(rel, "..") || strings.Contains(rel, string(filepath.Separator)) {
		t.Errorf("%q is not a direct child of the cache root (%v)", a, err)
	}
	// Case-insensitive disks: Foo/x and foo/x are one cache.
	b, _ := g.CachePath("WSTEIN/Workharbor")
	if a != b {
		t.Errorf("case variants map to different caches: %q and %q", a, b)
	}
	if other, _ := g.CachePath("wstein/workharbor2"); other == a {
		t.Error("different repositories share a cache")
	}
	if again, _ := g.CachePath("wstein/workharbor"); again != a {
		t.Error("the mapping is not stable")
	}

	for _, bad := range []string{
		"", "workharbor", "a/b/c", "../x/y", "a/..", "./a/b", "a/.", "a b/c", "a/b c", "ä/b", "a/ö", "K/x", ".hidden/x",
		"-flag/x", "a/-flag", "a/b\x00", "a\nb/c", "/a/b", "a/b/", "a\\b/c", "a/b%2e%2e", strings.Repeat("a", 101) + "/x", "a/" + strings.Repeat("b", 101),
	} {
		if p, err := g.CachePath(bad); !errors.Is(err, ErrBadName) {
			t.Errorf("CachePath(%q) = %q, %v; want ErrBadName", bad, p, err)
		}
	}
	if _, err := newGit(t).CachePath("a/b"); !errors.Is(err, ErrNoCacheRoot) {
		t.Errorf("without a cache root = %v, want ErrNoCacheRoot", err)
	}
}

func TestCachePathsMustBeAbsoluteAndInsideTheCacheRoot(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.MkdirTemp("", "src-") // outside the workspace root
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(src) })
	g := newGit(t, WithCacheRoot(root), WithWorkspaceRoot(t.TempDir()))
	for name, path := range map[string]string{
		"relative":     "cache.git",
		"a dot path":   ".",
		"outside":      filepath.Join(t.TempDir(), "cache.git"),
		"traversal":    filepath.Join(root, "..", "elsewhere.git"),
		"the root":     root,
		"empty":        "",
		"a nested dir": filepath.Join(root, "x", "y.git"),
	} {
		if _, err := g.OpenCache(ctx, path, CacheConfig{Source: src}); err == nil {
			t.Errorf("%s: OpenCache(%q) was accepted", name, path)
		}
	}
	if _, err := os.Stat("/cache.git"); err == nil {
		t.Error("a relative cache name was created under /")
	}
	good, _ := g.CachePath("wstein/workharbor")
	if _, err := g.OpenCache(ctx, good, CacheConfig{Source: src}); err != nil {
		t.Errorf("a mapped path: %v", err)
	}
	// A source inside the workspace root is agent-writable and must not feed a cache.
	ws := t.TempDir()
	strict := newGit(t, WithCacheRoot(root), WithWorkspaceRoot(ws))
	inside := filepath.Join(ws, "checkout")
	if err := os.MkdirAll(inside, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := strict.OpenCache(ctx, good+"-2", CacheConfig{Source: inside}); !errors.Is(err, ErrBadSource) {
		t.Errorf("a source inside the workspace root = %v, want ErrBadSource", err)
	}
}
