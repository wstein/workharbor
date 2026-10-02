package hostgit

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bundleRig is a supervisor repository that has the base commit, and a "guest"
// clone with agent commits that a bundle is made from.
type bundleRig struct {
	t     *testing.T
	g     *Git
	repo  *Repo
	guest string
	env   []string
	base  string
}

func newBundleRig(t *testing.T) *bundleRig {
	t.Helper()
	g := newGit(t)
	f := newForge(t, "", 2)
	dir, err := os.MkdirTemp("", "bundle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	repo, err := g.InitBare(context.Background(), filepath.Join(dir, "sup.git"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Run(context.Background(), "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", f.dir, "+refs/heads/main:refs/heads/main"); err == nil {
		t.Fatal("a plain fetch from a path must be refused by the floor: only the file transport of the caller may be used")
	}
	if _, err := repo.g.run(context.Background(), repo.path, true, nil, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--", f.dir, "+refs/heads/main:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	r := &bundleRig{t: t, g: g, repo: repo, guest: filepath.Join(dir, "guest"), env: plainEnv(filepath.Join(dir, "home"))}
	if err := os.MkdirAll(filepath.Join(dir, "home"), 0o750); err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.env, dir, "clone", "--quiet", "--no-local", f.dir, r.guest)
	r.base = mustGit(t, r.env, r.guest, "rev-parse", "HEAD")
	mustGit(t, r.env, r.guest, "checkout", "--quiet", "-b", "agent/docs")
	return r
}

// commit adds n commits on the guest branch; with blob it also adds a random file.
func (r *bundleRig) commit(n int, blob int) {
	r.t.Helper()
	for i := range n {
		name := filepath.Join(r.guest, "f.txt")
		if err := os.WriteFile(name, []byte(strings.Repeat("x", i+1)+"\n"), 0o600); err != nil {
			r.t.Fatal(err)
		}
		mustGit(r.t, r.env, r.guest, "add", "f.txt")
		mustGit(r.t, r.env, r.guest, "commit", "--quiet", "-m", "agent "+strings.Repeat("c", i+1))
	}
	if blob > 0 {
		b := make([]byte, blob)
		if _, err := rand.Read(b); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.guest, "blob.bin"), b, 0o600); err != nil {
			r.t.Fatal(err)
		}
		mustGit(r.t, r.env, r.guest, "add", "blob.bin")
		mustGit(r.t, r.env, r.guest, "commit", "--quiet", "-m", "blob")
	}
}

// raw makes the bundle with its exact bytes.
func (r *bundleRig) raw() []byte {
	r.t.Helper()
	path := filepath.Join(filepath.Dir(r.guest), "out.bundle")
	mustGit(r.t, r.env, r.guest, "bundle", "create", path, r.base+"..agent/docs")
	b, err := os.ReadFile(path) //nolint:gosec // a file the test just made
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

func (r *bundleRig) hasRef(branch string) bool {
	_, err := r.repo.Run(context.Background(), "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

func TestImportBundleTakesTheBranchAndNothingOfTheWorkspace(t *testing.T) {
	t.Parallel()
	r := newBundleRig(t)
	r.commit(3, 0)
	want := mustGit(t, r.env, r.guest, "rev-parse", "agent/docs")
	data := r.raw()

	// What a compromised guest leaves in its .git: it must never run on the host.
	markers := t.TempDir()
	mustGit(t, r.env, r.guest, "config", "core.fsmonitor", "touch "+filepath.Join(markers, "fsmonitor"))
	mustGit(t, r.env, r.guest, "config", "filter.x.clean", "touch "+filepath.Join(markers, "filter")+"; cat")
	for _, h := range []string{"post-checkout", "reference-transaction", "pre-commit"} {
		p := filepath.Join(r.guest, ".git", "hooks", h)
		if err := os.WriteFile(p, []byte("#!/bin/sh\ntouch "+filepath.Join(markers, h)+"\n"), 0o700); err != nil { //nolint:gosec // a planted hook in a test
			t.Fatal(err)
		}
	}

	got, err := r.repo.ImportBundle(context.Background(), "agent/docs", bytes.NewReader(data), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("imported %s, want %s", got, want)
	}
	if out := mustGit(t, r.env, r.repo.path, "rev-list", "--count", r.base+"..agent/docs"); out != "3" {
		t.Errorf("%s commits imported, want 3", out)
	}
	if entries, _ := os.ReadDir(markers); len(entries) != 0 {
		t.Errorf("something planted in the workspace ran on the host: %v", entries)
	}
	// An import again of the same bundle is fine.
	if again, err := r.repo.ImportBundle(context.Background(), "agent/docs", bytes.NewReader(data), 1<<20); err != nil || again != want {
		t.Errorf("second import: %s, %v", again, err)
	}
}

func TestImportBundleRefusesWhatIsNotWhole(t *testing.T) {
	t.Parallel()
	r := newBundleRig(t)
	r.commit(2, 600_000)
	data := r.raw()
	ctx := context.Background()

	t.Run("larger than the limit", func(t *testing.T) {
		_, err := r.repo.ImportBundle(ctx, "agent/docs", bytes.NewReader(data), int64(len(data))-1)
		if !errors.Is(err, ErrBundleTooLarge) {
			t.Errorf("err = %v", err)
		}
		if r.hasRef("agent/docs") {
			t.Error("a ref was left")
		}
	})
	t.Run("exactly the limit is accepted", func(t *testing.T) {
		repo := newBundleRigRepoOnly(t, r)
		if _, err := repo.ImportBundle(ctx, "agent/docs", bytes.NewReader(data), int64(len(data))); err != nil {
			t.Errorf("a bundle of exactly the limit: %v", err)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		repo := newBundleRigRepoOnly(t, r)
		if _, err := repo.ImportBundle(ctx, "agent/docs", bytes.NewReader(data[:len(data)/2]), 1<<24); err == nil {
			t.Error("a bundle cut in half was accepted")
		}
		if _, err := repo.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/agent/docs"); err == nil {
			t.Error("a ref was left by a refused bundle")
		}
	})
	t.Run("corrupted in the middle", func(t *testing.T) {
		repo := newBundleRigRepoOnly(t, r)
		bad := append([]byte(nil), data...)
		for i := len(bad) / 2; i < len(bad)/2+4096; i++ {
			bad[i] ^= 0xff
		}
		if _, err := repo.ImportBundle(ctx, "agent/docs", bytes.NewReader(bad), 1<<24); err == nil {
			t.Error("a corrupted bundle was accepted")
		}
		if _, err := repo.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/agent/docs"); err == nil {
			t.Error("a ref was left by a refused bundle")
		}
	})
	t.Run("garbage", func(t *testing.T) {
		repo := newBundleRigRepoOnly(t, r)
		if _, err := repo.ImportBundle(ctx, "agent/docs", strings.NewReader("not a bundle"), 1<<20); err == nil {
			t.Error("garbage was accepted")
		}
	})
	t.Run("another branch", func(t *testing.T) {
		repo := newBundleRigRepoOnly(t, r)
		if _, err := repo.ImportBundle(ctx, "agent/other", bytes.NewReader(data), 1<<24); !errors.Is(err, ErrBundleBranch) {
			t.Errorf("err = %v, want ErrBundleBranch", err)
		}
	})
	t.Run("bad arguments", func(t *testing.T) {
		repo := newBundleRigRepoOnly(t, r)
		if _, err := repo.ImportBundle(ctx, "-x", bytes.NewReader(data), 1<<20); !errors.Is(err, ErrBadBranch) {
			t.Errorf("a bad branch: %v", err)
		}
		if _, err := repo.ImportBundle(ctx, "agent/docs", bytes.NewReader(data), 0); err == nil {
			t.Error("no limit was accepted")
		}
	})
}

// newBundleRigRepoOnly returns a second supervisor repository that has the
// base commit and nothing else, so a refused bundle cannot be hidden by
// objects an earlier import brought in.
func newBundleRigRepoOnly(t *testing.T, r *bundleRig) *Repo {
	t.Helper()
	dir, err := os.MkdirTemp("", "bundle2-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	repo, err := r.g.InitBare(context.Background(), filepath.Join(dir, "sup.git"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.g.run(context.Background(), repo.path, true, nil, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--", filepath.Join(filepath.Dir(r.guest), "guest"), "+"+r.base+":refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	return repo
}
