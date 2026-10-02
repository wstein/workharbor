package hostgit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

// plainEnv is the environment of the control runs: plain git with no host
// config, so the planted settings are the only ones in play.
// plainEnv is the environment of a plain git, as an operator would run it, but
// isolated: nothing of the human's (no agent, no credential helper, no system
// configuration), so a hostile repository's own configuration is all that acts.
func plainEnv(home string) []string {
	return gittest.Env(home,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
}

func mustGit(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-c", "credential.helper="}, args...)...) //nolint:gosec // test helper; the arguments are built by the test
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// plant is the set of settings a hostile guest can leave in a checkout.
// Every one of them runs a script that drops a file into the canary directory.
type plant struct {
	repo   string // the agent's checkout
	canary string // directory the scripts write to
	sha    string // the commit on agent/topic
}

func (p plant) fired() []string {
	entries, _ := os.ReadDir(p.canary)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// newPlant builds an agent checkout on branch agent/topic with a hook, a
// file-system monitor, an ssh command, a pager, an editor, a credential
// helper, a clean and smudge filter and a pack-objects hook planted.
func newPlant(t *testing.T) plant {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	canary := filepath.Join(base, "canary")
	repo := filepath.Join(base, "agent-checkout")
	for _, d := range []string{home, canary, repo, filepath.Join(base, "evil")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	env := plainEnv(home)
	git := func(args ...string) string { return mustGit(t, env, repo, args...) }

	script := func(name, extra string) string {
		path := filepath.Join(base, "evil", name)
		body := "#!/bin/sh\n: > '" + filepath.Join(canary, name) + "'\n" + extra + "\n"
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil { //nolint:gosec // an executable test script
			t.Fatal(err)
		}
		return path
	}

	git("init", "--quiet", "-b", "agent/topic")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.go filter=evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".gitattributes", "main.go")
	git("commit", "--quiet", "-m", "agent work")
	sha := git("rev-parse", "HEAD")

	// Planted after the commit, as a guest would do it.
	hooks := filepath.Join(base, "evil", "hooks")
	if err := os.MkdirAll(hooks, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"pre-commit", "post-commit", "post-checkout", "reference-transaction", "pre-push", "pre-rebase"} {
		body := "#!/bin/sh\n: > '" + filepath.Join(canary, "hook-"+h) + "'\nexit 0\n"
		if err := os.WriteFile(filepath.Join(hooks, h), []byte(body), 0o700); err != nil { //nolint:gosec // an executable test hook
			t.Fatal(err)
		}
	}
	git("config", "core.hooksPath", hooks)
	git("config", "core.fsmonitor", script("fsmonitor", "exit 1"))
	git("config", "core.sshCommand", script("sshcommand", "exit 1"))
	git("config", "core.pager", script("pager", "cat"))
	git("config", "core.editor", script("editor", "exit 0"))
	git("config", "credential.helper", "!"+script("credential", "exit 0"))
	git("config", "filter.evil.clean", script("filter-clean", "cat"))
	git("config", "filter.evil.smudge", script("filter-smudge", "cat"))
	git("config", "uploadpack.packObjectsHook", script("packobjectshook", "exec \"$@\""))
	git("config", "core.alternateRefsCommand", script("alternaterefs", "exit 0"))

	return plant{repo: repo, canary: canary, sha: sha}
}

// The control: plain git fires every plant, so a test that finds the canary
// directory empty after hostgit has proven something.
func TestPlainGitFiresThePlants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		args  []string
		stdin string
		want  string // the canary file that must appear
	}{
		{"hook", []string{"commit", "--allow-empty", "-m", "x"}, "", "hook-pre-commit"},
		{"fsmonitor", []string{"status"}, "", "fsmonitor"},
		{"ssh command", []string{"ls-remote", "ssh://example.invalid/x"}, "", "sshcommand"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPlant(t)
			cmd := exec.CommandContext(context.Background(), "git", tc.args...) //nolint:gosec // arguments come from the test table
			cmd.Dir = p.repo
			cmd.Env = plainEnv(t.TempDir())
			cmd.Stdin = strings.NewReader(tc.stdin)
			_, _ = cmd.CombinedOutput() // several of these fail on purpose
			if got := p.fired(); !contains(got, tc.want) {
				t.Fatalf("plain git %v fired %v, want %q among them: the test setup is not hostile", tc.args, got, tc.want)
			}
		})
	}

	t.Run("filter", func(t *testing.T) {
		p := newPlant(t)
		if err := os.WriteFile(filepath.Join(p.repo, "new.go"), []byte("package main\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(context.Background(), "git", "add", "-A")
		cmd.Dir = p.repo
		cmd.Env = plainEnv(t.TempDir())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if got := p.fired(); !contains(got, "filter-clean") {
			t.Fatalf("plain git add fired %v, want filter-clean", got)
		}
	})

	t.Run("pager", func(t *testing.T) {
		p := newPlant(t)
		cmd := exec.CommandContext(context.Background(), "git", "var", "GIT_PAGER")
		cmd.Dir = p.repo
		cmd.Env = plainEnv(t.TempDir())
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "pager") {
			t.Fatalf("plain git would page through %q, want the planted script", strings.TrimSpace(string(out)))
		}
	})
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// newGit returns a Git. Without options its workspace root is the directory
// that holds every t.TempDir of this test run; with options, only those are set.
func newGit(t *testing.T, opts ...Option) *Git {
	t.Helper()
	if len(opts) == 0 {
		opts = []Option{WithWorkspaceRoot(filepath.Dir(t.TempDir()))}
	}
	g, err := New(opts...)
	if err != nil {
		t.Skipf("git is not available: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// hardened runs git the way hostgit does, but with arguments the wrappers
// would refuse, to test the floor on its own.
func hardened(t *testing.T, g *Git, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := g.command(context.Background(), dir, false, nil, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestTheFloorStopsTheKnownKeys(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	t.Run("hook", func(t *testing.T) {
		p := newPlant(t)
		if _, err := hardened(t, g, p.repo, "-c", "user.name=t", "-c", "user.email=t@example.test", "commit", "--allow-empty", "-m", "x"); err != nil {
			t.Fatal(err)
		}
		for _, name := range p.fired() {
			if strings.HasPrefix(name, "hook-") {
				t.Errorf("hardened commit ran the planted hook %s", name)
			}
		}
	})
	t.Run("fsmonitor", func(t *testing.T) {
		p := newPlant(t)
		_, _ = hardened(t, g, p.repo, "status")
		if got := p.fired(); contains(got, "fsmonitor") {
			t.Errorf("hardened status ran the file-system monitor: %v", got)
		}
	})
	t.Run("ssh command", func(t *testing.T) {
		p := newPlant(t)
		out, err := hardened(t, g, p.repo, "ls-remote", "ssh://example.invalid/x")
		if err == nil {
			t.Fatal("ls-remote over ssh must be refused")
		}
		if got := p.fired(); len(got) != 0 {
			t.Errorf("hardened ls-remote ran %v (%s)", got, out)
		}
		if !strings.Contains(out, "not allowed") {
			t.Errorf("expected the transport to be refused, got: %s", out)
		}
	})
	t.Run("pager", func(t *testing.T) {
		p := newPlant(t)
		out, err := hardened(t, g, p.repo, "var", "GIT_PAGER")
		if err != nil {
			t.Fatal(err)
		}
		if out != "cat" {
			t.Errorf("effective pager = %q, want cat", out)
		}
	})
	t.Run("the environment is empty of host variables", func(t *testing.T) {
		for _, e := range g.Env() {
			if strings.HasPrefix(e, "GIT_SSH") || strings.HasPrefix(e, "GIT_EXTERNAL_DIFF") || strings.HasPrefix(e, "GIT_ASKPASS") {
				t.Errorf("environment sets %s", e)
			}
		}
	})
}

// The host never reads the system or the global git configuration: Homebrew's
// system gitconfig names the osxkeychain credential helper, and an explicit
// `git config --system` ignores GIT_CONFIG_NOSYSTEM. This only reads the
// environment hostgit builds; it starts no process.
func TestTheEnvironmentPointsEveryConfigFileNowhere(t *testing.T) {
	t.Parallel()
	g, err := New()
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	env := strings.Join(g.Env(), "\n")
	for _, want := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(env, want) {
			t.Errorf("hostgit's environment lacks %s", want)
		}
	}
	if !strings.Contains(strings.Join(g.Config(false), "\n"), "credential.helper=") {
		t.Error("hostgit starts git with a credential helper")
	}
}
