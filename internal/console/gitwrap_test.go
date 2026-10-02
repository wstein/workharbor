package console

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitBox is a repository with a console-like home, where a test plants the
// settings an agent could write and runs git, plain or through the wrapper.
type gitBox struct {
	t       *testing.T
	git     string // the real git
	wrapper string // the whr-git script
	home    string
	repo    string
	stdin   string // what the next command reads
	marks   string // a command that ran touches a file here
}

func newGitBox(t *testing.T) *gitBox {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not installed")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := &gitBox{t: t, git: git, home: filepath.Join(dir, "home"), repo: filepath.Join(dir, "repo"), marks: filepath.Join(dir, "marks"), wrapper: filepath.Join(dir, "git")}
	for _, d := range []string{b.home, b.marks, b.repo} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(b.wrapper, GitWrapper(), 0o700); err != nil { //nolint:gosec // a script the test runs
		t.Fatal(err)
	}
	b.plain("init", "-q", "-b", "main", b.repo)
	b.write("a.txt", "one\n")
	b.plain("add", "a.txt")
	b.plain("commit", "-q", "-m", "one")
	return b
}

// env is the whole environment of a command: a short list, never os.Environ()
// with a changed HOME (AGENTS.md: Homebrew's git turns the keychain helper on,
// and a temporary HOME makes macOS offer to reset the human's keychain). No
// system or global configuration but the test's own, no credential helper, no
// prompt, no SSH agent.
func (b *gitBox) env(extra ...string) []string {
	return append([]string{
		"PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.TempDir(), "HOME=" + b.home, "TERM=xterm",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=" + filepath.Join(b.home, ".gitconfig"),
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
		"GIT_TERMINAL_PROMPT=0", "SSH_AUTH_SOCK=", "GIT_ASKPASS=", "SSH_ASKPASS=",
		"GIT_AUTHOR_NAME=h", "GIT_AUTHOR_EMAIL=h@example.com", "GIT_COMMITTER_NAME=h", "GIT_COMMITTER_EMAIL=h@example.com",
		"WHR_REAL_GIT=" + b.git, "VISUAL=true", "EDITOR=true",
	}, extra...)
}

func (b *gitBox) run(bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // a test running git or the wrapper in its own temporary directory
	cmd.Dir = b.repo
	cmd.Env = b.env()
	cmd.Stdin = strings.NewReader(b.stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// plain runs the real git and fails the test if it fails.
func (b *gitBox) plain(args ...string) {
	b.t.Helper()
	if out, err := b.run(b.git, args...); err != nil {
		b.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (b *gitBox) write(name, content string) {
	b.t.Helper()
	if err := os.WriteFile(filepath.Join(b.repo, name), []byte(content), 0o600); err != nil {
		b.t.Fatal(err)
	}
}

func (b *gitBox) config(key, value string) { b.t.Helper(); b.plain("config", key, value) }

// touch is a command that records it ran by creating a file named after it.
func (b *gitBox) touch(name string) string { return "touch " + filepath.Join(b.marks, name) }

func (b *gitBox) ran(name string) bool {
	_, err := os.Stat(filepath.Join(b.marks, name))
	return err == nil
}

func (b *gitBox) reset() {
	entries, _ := os.ReadDir(b.marks)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(b.marks, e.Name()))
	}
}

func (b *gitBox) hook(name string) {
	b.t.Helper()
	path := filepath.Join(b.repo, ".git", "hooks", name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+b.touch("hook-"+name)+"\n"), 0o700); err != nil { //nolint:gosec // a planted hook
		b.t.Fatal(err)
	}
}

func (b *gitBox) attributes(lines string) {
	b.t.Helper()
	if err := os.WriteFile(filepath.Join(b.repo, ".git", "info", "attributes"), []byte(lines), 0o600); err != nil {
		b.t.Fatal(err)
	}
}

// A kind is one way a repository makes git run a command: what is planted, and
// the git command that triggers it. The same plant must run under plain git and
// must not run under the wrapper.
type kind struct {
	name    string
	marker  string
	plant   func(b *gitBox)
	trigger func(b *gitBox, bin string)
}

func kinds() []kind {
	run := func(b *gitBox, bin string, args ...string) { _, _ = b.run(bin, args...) }
	return []kind{
		{
			"pre-commit hook", "hook-pre-commit", func(b *gitBox) { b.hook("pre-commit") },
			func(b *gitBox, bin string) { run(b, bin, "commit", "-q", "--allow-empty", "-m", "x") },
		},
		{
			"post-checkout hook", "hook-post-checkout", func(b *gitBox) { b.hook("post-checkout") },
			func(b *gitBox, bin string) { run(b, bin, "checkout", "-q", "-b", "other") },
		},
		{
			"fsmonitor", "fsmonitor", func(b *gitBox) { b.config("core.fsmonitor", "sh -c '"+b.touch("fsmonitor")+"; true'") },
			func(b *gitBox, bin string) { run(b, bin, "status") },
		},
		{"clean filter", "filter-clean", func(b *gitBox) {
			b.config("filter.x.clean", "sh -c '"+b.touch("filter-clean")+"; cat'")
			b.attributes("*.txt filter=x\n")
		}, func(b *gitBox, bin string) { b.write("a.txt", "two\n"); run(b, bin, "add", "a.txt") }},
		{"smudge filter", "filter-smudge", func(b *gitBox) {
			b.config("filter.x.smudge", "sh -c '"+b.touch("filter-smudge")+"; cat'")
			b.attributes("*.txt filter=x\n")
		}, func(b *gitBox, bin string) {
			_ = os.Remove(filepath.Join(b.repo, "a.txt"))
			run(b, bin, "checkout", "--", "a.txt")
		}},
		{"filter process", "filter-process", func(b *gitBox) {
			b.config("filter.x.process", "sh -c '"+b.touch("filter-process")+"; exit 1'")
			b.attributes("*.txt filter=x\n")
		}, func(b *gitBox, bin string) { b.write("a.txt", "two\n"); run(b, bin, "add", "a.txt") }},
		{"textconv", "textconv", func(b *gitBox) {
			b.config("diff.y.textconv", "sh -c '"+b.touch("textconv")+"; cat \"$1\"' --")
			b.attributes("*.txt diff=y\n")
		}, func(b *gitBox, bin string) { b.write("a.txt", "two\n"); run(b, bin, "diff", "HEAD") }},
		{"diff command", "diffcmd", func(b *gitBox) {
			b.config("diff.y.command", "sh -c '"+b.touch("diffcmd")+"' --")
			b.attributes("*.txt diff=y\n")
		}, func(b *gitBox, bin string) { b.write("a.txt", "two\n"); run(b, bin, "diff", "HEAD") }},
		{
			"external diff", "extdiff", func(b *gitBox) { b.config("diff.external", "sh -c '"+b.touch("extdiff")+"' --") },
			func(b *gitBox, bin string) { b.write("a.txt", "two\n"); run(b, bin, "diff", "HEAD") },
		},
		{
			"alias", "alias", func(b *gitBox) { b.config("alias.st", "!"+b.touch("alias")) },
			func(b *gitBox, bin string) { run(b, bin, "st") },
		},
		{"merge driver", "merge", func(b *gitBox) {
			b.config("merge.z.driver", "sh -c '"+b.touch("merge")+"; exit 1' -- %O %A %B")
			b.attributes("*.txt merge=z\n")
		}, func(b *gitBox, bin string) {
			b.plain("checkout", "-q", "-b", "side")
			b.write("a.txt", "side\n")
			b.plain("commit", "-q", "-a", "-m", "side")
			b.plain("checkout", "-q", "main")
			b.write("a.txt", "main\n")
			b.plain("commit", "-q", "-a", "-m", "main")
			run(b, bin, "merge", "side")
		}},
		{
			"trailer command", "trailer", func(b *gitBox) { b.config("trailer.t.cmd", "sh -c '"+b.touch("trailer")+"; echo v' --") },
			func(b *gitBox, bin string) { run(b, bin, "interpret-trailers", "--trailer", "t") },
		},
		{"remote uploadpack", "uploadpack", func(b *gitBox) {
			b.config("remote.r.url", b.repo)
			b.config("remote.r.uploadpack", "sh -c '"+b.touch("uploadpack")+"; exec git-upload-pack \"$@\"' --")
		}, func(b *gitBox, bin string) { run(b, bin, "fetch", "-q", "r") }},
		{
			"gpg program", "gpg", func(b *gitBox) {
				script := filepath.Join(b.marks, "gpg.sh")                                                             // git runs the program by path, not through a shell
				if err := os.WriteFile(script, []byte("#!/bin/sh\n"+b.touch("gpg")+"\nexit 1\n"), 0o700); err != nil { //nolint:gosec // a planted program
					b.t.Fatal(err)
				}
				b.config("gpg.program", script)
			},
			func(b *gitBox, bin string) { run(b, bin, "commit", "-q", "-S", "--allow-empty", "-m", "x") },
		},
		{
			"ssh command", "ssh", func(b *gitBox) { b.config("core.sshCommand", "sh -c '"+b.touch("ssh")+"; exit 1' --") },
			func(b *gitBox, bin string) { run(b, bin, "ls-remote", "ssh://localhost:1/x") },
		},
	}
}

// Spike #89 planted a hook, fsmonitor, a clean filter, a textconv and an alias:
// all ran under plain git, and only the first two stopped with GIT_CONFIG_COUNT
// alone. Each kind is planted twice: plain git must run it (or the test proves
// nothing), and the wrapper must not.
func TestTheWrapperStopsEverythingARepositoryCanPlant(t *testing.T) {
	for _, k := range kinds() {
		t.Run(k.name, func(t *testing.T) {
			plain := newGitBox(t)
			k.plant(plain)
			k.trigger(plain, plain.git)
			if !plain.ran(k.marker) {
				t.Fatalf("plain git did not run the planted %s: the plant does not work", k.name)
			}

			wrapped := newGitBox(t)
			k.plant(wrapped)
			k.trigger(wrapped, wrapped.wrapper)
			if wrapped.ran(k.marker) {
				t.Errorf("the planted %s ran under the wrapper", k.name)
			}
		})
	}
}

// The wrapper is git: ordinary work in a repository goes through it.
func TestTheWrapperRunsOrdinaryGit(t *testing.T) {
	b := newGitBox(t)
	b.write("b.txt", "hello\n")
	for _, args := range [][]string{{"add", "b.txt"}, {"commit", "-q", "-m", "two"}, {"log", "--oneline"}, {"status", "--short"}, {"diff", "HEAD~1"}} {
		if out, err := b.run(b.wrapper, args...); err != nil {
			t.Fatalf("git %v through the wrapper: %v\n%s", args, err, out)
		}
	}
	if out, _ := b.run(b.wrapper, "log", "--oneline"); strings.Count(strings.TrimSpace(out), "\n") != 1 {
		t.Errorf("log = %q, want two commits", out)
	}
	if out, err := b.run(b.wrapper, "--version"); err != nil || !strings.HasPrefix(out, "git version") {
		t.Errorf("--version = %q, %v", out, err)
	}
}

// The human's own configuration keeps working, and a repository cannot take a
// setting of it over by naming the same key.
func TestTheHumansOwnSettingsStillWork(t *testing.T) {
	b := newGitBox(t)
	if err := os.WriteFile(filepath.Join(b.home, ".gitconfig"), []byte("[alias]\n\thi = !echo hello from home\n\tdt = diff --stat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.config("alias.hi", "!"+b.touch("repo-alias")) // the repository redefines the human's alias
	b.config("alias.mine", "!"+b.touch("repo-only-alias"))

	out, err := b.run(b.wrapper, "hi")
	if err != nil || strings.TrimSpace(out) != "hello from home" {
		t.Errorf("the human's alias: %q, %v", out, err)
	}
	if b.ran("repo-alias") {
		t.Error("the repository took over the human's alias")
	}
	out, err = b.run(b.wrapper, "mine")
	if err == nil || b.ran("repo-only-alias") || !strings.Contains(out, "alias from the repository is disabled") {
		t.Errorf("an alias only the repository has must be disabled: %q, %v", out, err)
	}
	if out, err := b.run(b.wrapper, "dt"); err != nil {
		t.Errorf("another alias of the human's: %q, %v", out, err)
	}
}

// A setting that comes in through an include is the repository's too, whatever
// the file is called.
func TestAnIncludedFileIsNotTrusted(t *testing.T) {
	b := newGitBox(t)
	inc := filepath.Join(b.repo, "evil.inc")
	if err := os.WriteFile(inc, []byte("[alias]\n\tst = !"+b.touch("included")+"\n[core]\n\tpager = included-pager\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.config("include.path", inc)
	if out, _ := b.run(b.git, "config", "--get", "core.pager"); strings.TrimSpace(out) != "included-pager" {
		t.Fatalf("the include does not reach git: core.pager = %q", out)
	}
	_, _ = b.run(b.git, "st")
	if !b.ran("included") {
		t.Fatal("plain git did not run the included alias: the plant does not work")
	}
	b.reset()
	_, _ = b.run(b.wrapper, "st")
	if b.ran("included") {
		t.Error("an alias from an included file ran")
	}
	if out, _ := b.run(b.wrapper, "config", "--get", "core.pager"); strings.TrimSpace(out) != "less" {
		t.Errorf("core.pager from an included file = %q, want less", out)
	}
}

// What the repository cannot do through the environment either: its settings do
// not reach git through GIT_CONFIG_COUNT as the caller set it, which is kept.
func TestTheCallersOwnOverridesAreKept(t *testing.T) {
	b := newGitBox(t)
	cmd := exec.Command(b.wrapper, "config", "--get", "user.name") //nolint:gosec,noctx // the test's wrapper
	cmd.Dir = b.repo
	cmd.Env = b.env("GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=from the caller")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "from the caller" {
		t.Errorf("user.name = %q, %v", out, err)
	}
	cmd = exec.Command(b.wrapper, "config", "--get", "core.hooksPath") //nolint:gosec,noctx // the test's wrapper
	cmd.Dir = b.repo
	cmd.Env = b.env("GIT_CONFIG_COUNT=bogus")
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "/dev/null" {
		t.Errorf("a garbled GIT_CONFIG_COUNT must not switch the protection off: %q, %v", out, err)
	}
}

func TestTheWrapperIsPlainShell(t *testing.T) {
	script := string(GitWrapper())
	if !strings.HasPrefix(script, "#!/bin/sh\n") {
		t.Error("the wrapper must start with #!/bin/sh")
	}
	for _, bashism := range []string{"[[", "read -d", "local ", "declare ", "<<<", "$'", "function "} {
		if strings.Contains(script, bashism) {
			t.Errorf("the wrapper uses %q: Ubuntu's /bin/sh is dash", bashism)
		}
	}
	if dash, err := exec.LookPath("dash"); err == nil {
		path := filepath.Join(t.TempDir(), "whr-git")
		if err := os.WriteFile(path, GitWrapper(), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(dash, "-n", path).CombinedOutput(); err != nil { //nolint:gosec,noctx // a syntax check
			t.Errorf("dash -n: %v\n%s", err, out)
		}
	}
}

// A setting git only uses with a terminal cannot be triggered in a test, so the
// check is on the value: the wrapper gives the key a neutral one, the plain
// repository the planted one.
func TestTheWrapperNeutralisesWhatNeedsATerminal(t *testing.T) {
	for _, k := range []struct {
		name    string
		plant   func(b *gitBox)
		neutral map[string]string
	}{
		{"pager", func(b *gitBox) { b.config("core.pager", "sh -c '"+b.touch("pager")+"; cat'") }, map[string]string{"core.pager": "less"}},
		{"pager of a command", func(b *gitBox) { b.config("pager.log", "sh -c '"+b.touch("cmdpager")+"; cat'") }, map[string]string{"pager.log": "false"}},
		// Never run a credential command in a test: a helper may reach the human's keychain.
		{"credential helper", func(b *gitBox) { b.config("credential.helper", "!"+b.touch("credential")+"; true") }, map[string]string{"credential.helper": ""}},
		{"editor", func(b *gitBox) {
			b.config("core.editor", "sh -c '"+b.touch("editor")+"' --")
			b.config("sequence.editor", "sh -c '"+b.touch("seqeditor")+"' --")
		}, map[string]string{"core.editor": "true", "sequence.editor": "true"}},
	} {
		t.Run(k.name, func(t *testing.T) {
			box := newGitBox(t)
			k.plant(box)
			for key, want := range k.neutral {
				if out, _ := box.run(box.git, "config", "--local", "--get", key); strings.TrimSpace(out) == want || strings.TrimSpace(out) == "" {
					t.Errorf("%s: the repository does not hold the planted value (%q)", key, out)
				}
				if out, _ := box.run(box.wrapper, "config", "--get", key); strings.TrimSpace(out) != want {
					t.Errorf("%s through the wrapper = %q, want %q", key, strings.TrimSpace(out), want)
				}
			}
		})
	}
}
