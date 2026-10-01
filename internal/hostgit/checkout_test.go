package hostgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A second repository the agent wants git to use instead of its own, holding a
// secret the agent must not be able to move into the supervisor's repo.
func secretRepo(t *testing.T, base string) (dir, blob string) {
	t.Helper()
	dir = filepath.Join(base, "secret-repo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	env := plainEnv(filepath.Join(base, "home"))
	mustGit(t, env, dir, "init", "--quiet", "-b", "agent/topic")
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("host-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, env, dir, "add", "secret.txt")
	mustGit(t, env, dir, "commit", "--quiet", "-m", "secret")
	return dir, mustGit(t, env, dir, "rev-parse", "HEAD:secret.txt")
}

func TestVerifyCheckoutRefusesRedirects(t *testing.T) {
	t.Run("a gitfile instead of a .git directory", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		other, _ := secretRepo(t, filepath.Dir(p.canary))
		gitDir := filepath.Join(p.repo, ".git")
		if err := os.RemoveAll(gitDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(gitDir, []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrCheckout) {
			t.Fatalf("Untrusted = %v, want ErrCheckout", err)
		}
	})

	t.Run("a .git symlink", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		other, _ := secretRepo(t, filepath.Dir(p.canary))
		gitDir := filepath.Join(p.repo, ".git")
		if err := os.RemoveAll(gitDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(other, ".git"), gitDir); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrCheckout) {
			t.Fatalf("Untrusted = %v, want ErrCheckout", err)
		}
	})

	t.Run("an objects symlink", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		other, _ := secretRepo(t, filepath.Dir(p.canary))
		objects := filepath.Join(p.repo, ".git", "objects")
		if err := os.RemoveAll(objects); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(other, ".git", "objects"), objects); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrCheckout) {
			t.Fatalf("Untrusted = %v, want ErrCheckout", err)
		}
	})

	t.Run("a commondir file", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		other, _ := secretRepo(t, filepath.Dir(p.canary))
		if err := os.WriteFile(filepath.Join(p.repo, ".git", "commondir"), []byte(filepath.Join(other, ".git")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrCheckout) {
			t.Fatalf("Untrusted = %v, want ErrCheckout", err)
		}
	})

	t.Run("a checkout outside the workspace root", func(t *testing.T) {
		p := newPlant(t)
		root := t.TempDir() // a different directory than the one holding the checkout
		strict := newGit(t, WithWorkspaceRoot(root))
		if _, err := strict.Untrusted(p.repo); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("Untrusted = %v, want ErrOutsideRoot", err)
		}
	})

	t.Run("a symlink that leaves the root", func(t *testing.T) {
		p := newPlant(t)
		root := t.TempDir()
		link := filepath.Join(root, "ws")
		if err := os.Symlink(p.repo, link); err != nil {
			t.Fatal(err)
		}
		strict := newGit(t, WithWorkspaceRoot(root))
		if _, err := strict.Untrusted(link); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("Untrusted = %v, want ErrOutsideRoot", err)
		}
	})

	t.Run("the root itself", func(t *testing.T) {
		root := t.TempDir()
		strict := newGit(t, WithWorkspaceRoot(root))
		if _, err := strict.Untrusted(root); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("Untrusted = %v, want ErrOutsideRoot", err)
		}
	})
}

func TestNoRootNoCheckout(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	if _, err := g.Untrusted(t.TempDir()); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("Untrusted without a root = %v, want ErrNoRoot", err)
	}
}

// The control: git follows a gitfile, so without the check the agent's checkout
// reads the other repository.
func TestControlGitFollowsAGitfile(t *testing.T) {
	p := newPlant(t)
	other, blob := secretRepo(t, filepath.Dir(p.canary))
	gitDir := filepath.Join(p.repo, ".git")
	if err := os.RemoveAll(gitDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitDir, []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := mustGit(t, plainEnv(filepath.Join(filepath.Dir(p.canary), "home")), p.repo, "cat-file", "-p", blob)
	if !strings.Contains(out, "host-secret") {
		t.Fatalf("the control did not read the other repository: %q", out)
	}
}

func TestFetchRefusesRedirectedCheckout(t *testing.T) {
	g := newGit(t)
	p := newPlant(t)
	other, _ := secretRepo(t, filepath.Dir(p.canary))
	gitDir := filepath.Join(p.repo, ".git")
	if err := os.RemoveAll(gitDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitDir, []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := g.InitBare(context.Background(), filepath.Join(t.TempDir(), "supervisor.git"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FetchBranch(context.Background(), p.repo, "agent/topic"); !errors.Is(err, ErrCheckout) {
		t.Fatalf("FetchBranch = %v, want ErrCheckout", err)
	}
}

func writeAlternates(t *testing.T, repo string, lines ...string) {
	t.Helper()
	path := filepath.Join(repo, ".git", "objects", "info")
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "alternates"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The control: with an alternates file naming the secret repo, fetching from
// the agent's checkout with plain git copies the host secret into the
// supervisor's bare repo. This is what the alternates check prevents.
func TestControlAlternatesExfiltrateWithPlainGit(t *testing.T) {
	p := newPlant(t)
	base := filepath.Dir(p.canary)
	other, blob := secretRepo(t, base)
	writeAlternates(t, p.repo, filepath.Join(other, ".git", "objects"))

	env := plainEnv(filepath.Join(base, "home"))
	bare := filepath.Join(base, "bare.git")
	mustGit(t, env, base, "init", "--bare", "--quiet", bare)
	// Make the agent's branch refer to the secret commit so that fetch needs its objects.
	secretCommit := mustGit(t, env, other, "rev-parse", "HEAD")
	mustGit(t, env, p.repo, "update-ref", "refs/heads/agent/topic", secretCommit)
	mustGit(t, env, bare, "fetch", "--quiet", filepath.Join(p.repo, ".git"), "+refs/heads/agent/topic:refs/heads/agent/topic")
	if out := mustGit(t, env, bare, "cat-file", "-p", blob); !strings.Contains(out, "host-secret") {
		t.Fatalf("the control did not copy the secret: %q", out)
	}
}

func TestAlternatesAreRefused(t *testing.T) {
	t.Run("an unlisted alternate", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		other, _ := secretRepo(t, filepath.Dir(p.canary))
		writeAlternates(t, p.repo, filepath.Join(other, ".git", "objects"))
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrAlternates) {
			t.Fatalf("Untrusted = %v, want ErrAlternates", err)
		}
	})

	t.Run("fetch never copies the secret", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		base := filepath.Dir(p.canary)
		other, blob := secretRepo(t, base)
		writeAlternates(t, p.repo, filepath.Join(other, ".git", "objects"))
		secretCommit := mustGit(t, plainEnv(filepath.Join(base, "home")), other, "rev-parse", "HEAD")
		mustGit(t, plainEnv(filepath.Join(base, "home")), p.repo, "update-ref", "refs/heads/agent/topic", secretCommit)
		r, err := g.InitBare(context.Background(), filepath.Join(base, "supervisor.git"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.FetchBranch(context.Background(), p.repo, "agent/topic"); !errors.Is(err, ErrAlternates) {
			t.Fatalf("FetchBranch = %v, want ErrAlternates", err)
		}
		if _, err := r.Run(context.Background(), "cat-file", "-e", blob); err == nil {
			t.Fatal("the secret blob is in the supervisor's repo")
		}
	})

	t.Run("a relative alternate", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		other, _ := secretRepo(t, filepath.Dir(p.canary))
		rel, err := filepath.Rel(filepath.Join(p.repo, ".git", "objects"), filepath.Join(other, ".git", "objects"))
		if err != nil {
			t.Fatal(err)
		}
		writeAlternates(t, p.repo, rel)
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrAlternates) {
			t.Fatalf("Untrusted = %v, want ErrAlternates", err)
		}
	})

	t.Run("a symlink to the listed cache is the cache", func(t *testing.T) {
		p := newPlant(t)
		cache := filepath.Join(filepath.Dir(p.canary), "cache")
		if err := os.MkdirAll(cache, 0o750); err != nil {
			t.Fatal(err)
		}
		g := newGit(t, WithAlternates(cache))
		writeAlternates(t, p.repo, cache)
		if _, err := g.Untrusted(p.repo); err != nil {
			t.Fatalf("a listed cache was refused: %v", err)
		}
	})

	t.Run("http-alternates", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		info := filepath.Join(p.repo, ".git", "objects", "info")
		if err := os.MkdirAll(info, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(info, "http-alternates"), []byte("http://example.invalid/\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrAlternates) {
			t.Fatalf("Untrusted = %v, want ErrAlternates", err)
		}
	})

	t.Run("a symlinked alternates file", func(t *testing.T) {
		g := newGit(t)
		p := newPlant(t)
		info := filepath.Join(p.repo, ".git", "objects", "info")
		if err := os.MkdirAll(info, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc/hosts", filepath.Join(info, "alternates")); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Untrusted(p.repo); !errors.Is(err, ErrAlternates) {
			t.Fatalf("Untrusted = %v, want ErrAlternates", err)
		}
	})
}
