package scripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

// Source install fixtures use a real isolated git repository and fake builds;
// they never replace an installed supervisor or read the human's git settings.
type sourceInstall struct {
	home, repo, prefix string
	env                []string
}

func (s sourceInstall) git(t *testing.T, args ...string) {
	t.Helper()
	cmd := gittest.Git(context.Background(), s.home, s.repo, gittest.Identity, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (s sourceInstall) install(t *testing.T) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "make", "install", "PREFIX="+s.prefix) //nolint:gosec // fixture-controlled arguments, no shell
	cmd.Dir, cmd.Env = s.repo, s.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func newSourceInstall(t *testing.T) sourceInstall {
	t.Helper()
	home, repo, bin := t.TempDir(), t.TempDir(), t.TempDir()
	env := gittest.Env(home, gittest.Identity...)
	git := func(args ...string) {
		t.Helper()
		cmd := gittest.Git(context.Background(), home, repo, gittest.Identity, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	for _, name := range []string{"Makefile", "scripts/install-source.go"} {
		data, err := os.ReadFile(filepath.Join("..", name)) //nolint:gosec // fixed repository source inventory
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0o600); err != nil { //nolint:gosec // fixed source names beneath the private fixture repository
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-m", "fixture")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("commit", "--allow-empty", "-m", "reviewed local main")
	prefix := filepath.Join(home, ".local")
	if err := os.Mkdir(prefix, 0o700); err != nil {
		t.Fatal(err)
	}
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\nif [ \"$1\" = run ] || [ \"$1\" = env ]; then exec '" + realGo + "' \"$@\"; fi\nwhile [ \"$1\" != -o ]; do shift; done\nshift\ndest=$1\nif [ -d \"$dest\" ]; then dest=$dest/whr; fi\nprintf '#!/bin/sh\\necho fixture-version\\n' > \"$dest\"\nchmod 700 \"$dest\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fake), 0o700); err != nil { //nolint:gosec // executable fixture
		t.Fatal(err)
	}
	return sourceInstall{home: home, repo: repo, prefix: prefix, env: append(env, "PATH="+bin+":"+os.Getenv("PATH"), "GOCACHE="+t.TempDir())}
}

func TestSourceInstallUnpublishedMain(t *testing.T) {
	s := newSourceInstall(t)
	version := filepath.Join(s.prefix, "libexec", "whr", "VERSION")
	if err := os.MkdirAll(filepath.Dir(version), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(version, []byte("old-release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := s.install(t)
	if err != nil {
		t.Fatalf("clean unpublished main must install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "development") {
		t.Fatalf("development installation was not disclosed:\n%s", out)
	}
	for _, name := range []string{"bin/whr", "libexec/whr/whr-shim-linux-arm64", "libexec/whr/whr-proxy-linux-arm64"} {
		if _, err := os.Stat(filepath.Join(s.prefix, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(version); !os.IsNotExist(err) {
		t.Fatalf("source install retained stale release VERSION: %v", err)
	}
}

func envValue(env []string, key string) string {
	v := ""
	for _, e := range env {
		if rest, ok := strings.CutPrefix(e, key+"="); ok {
			v = rest
		}
	}
	return v
}

func TestSourceInstallSignsAndReplacesByRename(t *testing.T) {
	s := newSourceInstall(t)
	bin := t.TempDir()
	log := filepath.Join(bin, "codesign.log")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "codesign"), []byte(fake), 0o700); err != nil { //nolint:gosec // executable fixture
		t.Fatal(err)
	}
	s.env = append(s.env, "PATH="+bin+":"+envValue(s.env, "PATH"))
	old := filepath.Join(s.prefix, "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho old\n"), 0o700); err != nil { //nolint:gosec // executable fixture
		t.Fatal(err)
	}
	before, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := s.install(t); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	after, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("whr was rewritten in place instead of replaced by rename")
	}
	signed, _ := os.ReadFile(log) //nolint:gosec // fixture path
	if runtime.GOOS == "darwin" && !strings.Contains(string(signed), "--force --sign -") {
		t.Errorf("darwin install did not ad-hoc sign whr: %q", signed)
	}
	if runtime.GOOS != "darwin" && len(signed) != 0 {
		t.Errorf("non-darwin install called codesign: %q", signed)
	}
	for _, dir := range []string{"bin", "libexec/whr"} {
		entries, err := os.ReadDir(filepath.Join(s.prefix, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".whr-install.") {
				t.Errorf("temporary %s left in %s", e.Name(), dir)
			}
		}
	}
}

func TestSourceInstallSmokeFailureNamesCodesign(t *testing.T) {
	s := newSourceInstall(t)
	bin := t.TempDir()
	// A go that builds a whr which exits non-zero, as a killed binary would.
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\nif [ \"$1\" = run ] || [ \"$1\" = env ]; then exec '" + realGo + "' \"$@\"; fi\nwhile [ \"$1\" != -o ]; do shift; done\nshift\nprintf '#!/bin/sh\\nexit 137\\n' > \"$1\"\nchmod 700 \"$1\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fake), 0o700); err != nil { //nolint:gosec // executable fixture
		t.Fatal(err)
	}
	s.env = append(s.env, "PATH="+bin+":"+envValue(s.env, "PATH"))
	out, err := s.install(t)
	if err == nil || !strings.Contains(out, "codesign -v") || !strings.Contains(out, "xattr -l") {
		t.Fatalf("a whr that does not run must fail naming codesign -v and xattr -l: %v\n%s", err, out)
	}
}

func TestSourceInstallDetachedMainDefaultPrefix(t *testing.T) {
	s := newSourceInstall(t)
	linked := t.TempDir()
	s.git(t, "worktree", "add", "--detach", linked, "main")
	s.repo = linked
	cmd := exec.CommandContext(context.Background(), "make", "install")
	cmd.Dir, cmd.Env = s.repo, s.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detached current main with default existing HOME.local: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(s.prefix, "bin", "whr")); err != nil {
		t.Fatal(err)
	}
}

func TestSourceInstallQuotesPrefixAndIgnoresGitSelectors(t *testing.T) {
	s := newSourceInstall(t)
	s.prefix = filepath.Join(s.home, "developer's `touch SHOULD_NOT_EXIST` prefix")
	if err := os.Mkdir(s.prefix, 0o700); err != nil {
		t.Fatal(err)
	}
	s.env = append(s.env, "GIT_DIR="+t.TempDir(), "GIT_WORK_TREE="+t.TempDir(), "GIT_INDEX_FILE="+filepath.Join(t.TempDir(), "index"))
	s.git(t, "replace", "HEAD", "HEAD~1")
	cmd := gittest.Git(context.Background(), s.home, s.repo, nil, "rev-parse", "--short=7", "HEAD")
	head, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.install(t)
	if err != nil {
		t.Fatalf("safe literal prefix and inherited Git selectors: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".Commit="+strings.TrimSpace(string(head))) || !strings.Contains(out, ".Dirty=false") || !strings.Contains(out, ".Version=v0.0.0-2-g") {
		t.Fatalf("stamp did not describe the guarded clean source:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(s.repo, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatalf("prefix executed shell text: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.prefix, "bin", "whr")); err != nil {
		t.Fatal(err)
	}
}

func TestSourceInstallRejectsUnsafeInputs(t *testing.T) {
	cache := t.TempDir()
	for _, tc := range []struct {
		name   string
		want   string
		change func(*testing.T, *sourceInstall)
	}{
		{"dirty", "dirty tree", func(t *testing.T, s *sourceInstall) {
			if err := os.WriteFile(filepath.Join(s.repo, "dirty"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"topic", "current local main", func(t *testing.T, s *sourceInstall) {
			s.git(t, "switch", "-c", "topic")
			s.git(t, "commit", "--allow-empty", "-m", "unmerged")
		}},
		{"outdated", "current local main", func(t *testing.T, s *sourceInstall) { s.git(t, "checkout", "--detach", "HEAD~1") }},
		{"hidden-untracked", "dirty tree", func(t *testing.T, s *sourceInstall) {
			s.git(t, "config", "status.showUntrackedFiles", "no")
			if err := os.WriteFile(filepath.Join(s.repo, "source.go"), []byte("untracked source"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"alternate-worktree", "physical source checkout", func(t *testing.T, s *sourceInstall) {
			alternate := t.TempDir()
			for _, name := range []string{"Makefile", "scripts/install-source.go"} {
				data, err := os.ReadFile(filepath.Join(s.repo, name)) //nolint:gosec // fixed source inventory in private fixture
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(alternate, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // fixed source names beneath private alternate fixture
					t.Fatal(err)
				}
			}
			s.git(t, "config", "core.worktree", alternate)
			f, err := os.OpenFile(filepath.Join(s.repo, "Makefile"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString("\n# modified physical source\n"); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{"checkout-prefix", "outside the source checkout", func(_ *testing.T, s *sourceInstall) { s.prefix = s.repo }},
		{"metadata-prefix", "outside the source checkout", func(_ *testing.T, s *sourceInstall) { s.prefix = filepath.Join(s.repo, ".git") }},
		{"home-prefix", "ancestor of HOME", func(_ *testing.T, s *sourceInstall) { s.prefix = s.home }},
		{"home-ancestor", "ancestor of HOME", func(_ *testing.T, s *sourceInstall) { s.prefix = filepath.Dir(s.home) }},
		{"root-prefix", "ancestor of HOME", func(_ *testing.T, s *sourceInstall) { s.prefix = "/" }},
		{"managed-unpublished", "signed install-release", func(_ *testing.T, s *sourceInstall) { s.prefix = "/opt/whr" }},
		{"managed-alias", "signed install-release", func(t *testing.T, s *sourceInstall) {
			s.prefix = filepath.Join(s.home, "managed")
			if err := os.Symlink("/usr/local", s.prefix); err != nil {
				t.Fatal(err)
			}
		}},
		{"managed-descendant-alias", "signed install-release", func(t *testing.T, s *sourceInstall) {
			if _, err := os.Stat("/usr/local/bin"); err != nil {
				t.Skip("no existing managed descendant")
			}
			s.prefix = filepath.Join(s.home, "managed-bin")
			if err := os.Symlink("/usr/local/bin", s.prefix); err != nil {
				t.Fatal(err)
			}
		}},
		{"binary-directory", "regular single-link file", func(t *testing.T, s *sourceInstall) {
			if err := os.MkdirAll(filepath.Join(s.prefix, "bin", "whr"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"version-directory", "regular single-link file", func(t *testing.T, s *sourceInstall) {
			if err := os.MkdirAll(filepath.Join(s.prefix, "libexec", "whr", "VERSION"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory-file", "must be a directory", func(t *testing.T, s *sourceInstall) {
			if err := os.WriteFile(filepath.Join(s.prefix, "bin"), []byte("existing"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"binary-redirection", "is a symlink", func(t *testing.T, s *sourceInstall) {
			if err := os.Mkdir(filepath.Join(s.prefix, "bin"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(s.home, "outside"), filepath.Join(s.prefix, "bin", "whr")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSourceInstall(t)
			s.env = append(s.env, "GOCACHE="+cache)
			tc.change(t, &s)
			binary := filepath.Join(s.home, ".local", "bin", "whr")
			_, before := os.Lstat(binary)
			out, err := s.install(t)
			if err == nil {
				t.Fatalf("unsafe install succeeded:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("wrong refusal, want %q:\n%s", tc.want, out)
			}
			if _, err := os.Lstat(binary); os.IsNotExist(before) && !os.IsNotExist(err) {
				t.Fatalf("refused install changed destination: %v", err)
			}
			if _, err := os.Stat(filepath.Join(binary, "whr")); err == nil {
				t.Fatal("refused install built a nested binary")
			}
		})
	}
}

// DESTDIR stages the install (#473): every write lands under it, the real PREFIX
// (here a path that does not exist, so it would be refused without DESTDIR) is never
// touched, and the staged tree carries no trace of DESTDIR in what it installs.
func TestSourceInstallDestdirStagesUnderDestdirOnly(t *testing.T) {
	s := newSourceInstall(t)
	const prefix = "/whr-473-destdir-prefix"
	destdir := t.TempDir()
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(context.Background(), "make", append([]string{"install", "PREFIX=" + prefix}, args...)...) //nolint:gosec // fixture-controlled arguments, no shell
		cmd.Dir, cmd.Env = s.repo, s.env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run(); err == nil {
		t.Fatalf("a missing PREFIX without DESTDIR must be refused:\n%s", out)
	}
	staleVersion := filepath.Join(destdir, prefix, "libexec", "whr", "VERSION")
	if err := os.MkdirAll(filepath.Dir(staleVersion), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleVersion, []byte("old-release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run("DESTDIR=" + destdir)
	if err != nil {
		t.Fatalf("staged install: %v\n%s", err, out)
	}
	for _, name := range []string{"bin/whr", "libexec/whr/whr-shim-linux-arm64", "libexec/whr/whr-proxy-linux-arm64"} {
		if _, err := os.Stat(filepath.Join(destdir, prefix, name)); err != nil {
			t.Errorf("missing staged %s: %v", name, err)
		}
	}
	if _, err := os.Stat(staleVersion); !os.IsNotExist(err) {
		t.Errorf("staged VERSION was kept: %v", err)
	}
	if _, err := os.Lstat(prefix); !os.IsNotExist(err) {
		t.Errorf("the real PREFIX was touched: %v", err)
	}
	if !strings.Contains(out, prefix+"/bin/whr setup") || strings.Contains(out, destdir+prefix+"/bin/whr setup") {
		t.Errorf("messages must name PREFIX, not DESTDIR:\n%s", out)
	}
	// Staging into a fresh DESTDIR, where the prefix does not exist yet, works too.
	fresh := t.TempDir()
	if out, err := run("DESTDIR=" + fresh); err != nil {
		t.Fatalf("staged install into an empty DESTDIR: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(fresh, prefix, "bin", "whr")); err != nil {
		t.Errorf("missing staged whr in an empty DESTDIR: %v", err)
	}
	for _, bad := range []string{"relative/dir", destdir + "/", "/", filepath.Join(destdir, "missing")} {
		if out, err := run("DESTDIR=" + bad); err == nil {
			t.Errorf("DESTDIR=%q must be refused:\n%s", bad, out)
		}
	}
}
