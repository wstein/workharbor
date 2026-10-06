package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

const (
	cut = "# ------------------------ >8 ------------------------"
	bot = "Claude <noreply@anthropic.com>"
)

func TestEachCallerPinsItsScissorsMode(t *testing.T) {
	if o := hookOptions(bot); !o.Scissors || o.Final || o.Author != bot {
		t.Errorf("hook options %+v", o)
	}
	if o := rangeOptions(bot); o.Scissors || !o.Final || o.Author != bot {
		t.Errorf("range options %+v", o)
	}
}

func TestTheHookCutsAtScissorsAndARangeDoesNot(t *testing.T) {
	// the hook: git commit cleanup has cut the message, so what follows is no trailer
	dir := t.TempDir()
	file := filepath.Join(dir, "COMMIT_EDITMSG")
	if err := os.WriteFile(file, []byte("docs: a\n\nRefs: #1\n"+cut+"\n\nSigned-off-by: P <p@example.test>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := lintFile(file, bot); got != 0 {
		t.Errorf("hook: exit %d, want 0", got)
	}
	if got := lintFile(file+".missing", bot); got != 2 {
		t.Errorf("missing file: exit %d, want 2", got)
	}

	// a stored commit keeps every line, so the same message is refused
	home := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	original := gitCommand
	gitCommand = func(args ...string) *exec.Cmd {
		return gittest.Git(context.Background(), home, repo, gittest.Identity, args...)
	}
	t.Cleanup(func() { gitCommand = original })
	run := func(args ...string) {
		t.Helper()
		out, err := gitCommand(args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "-b", "main")
	run("commit", "--quiet", "--allow-empty", "-m", "docs: base")
	for name, msg := range map[string]string{
		"after":  "docs: a\n\nRefs: #1\n" + cut + "\n\nSigned-off-by: P <p@example.test>",
		"before": "docs: a\n\nSigned-off-by: P <p@example.test>\n" + cut + "\n\nprose",
	} {
		run("commit", "--quiet", "--allow-empty", "--cleanup=verbatim", "--author="+bot, "-m", msg)
		if got := lintRange("HEAD~1..HEAD"); got != 1 {
			t.Errorf("range, signoff %s the line: exit %d, want 1", name, got)
		}
		run("reset", "--quiet", "--hard", "HEAD~1")
	}
	run("commit", "--quiet", "--allow-empty", "--cleanup=verbatim", "--author="+bot, "-m", "docs: a\n\nRefs: #1\n"+cut+"\n\nno trailer")
	if got := lintRange("HEAD~1..HEAD"); got != 0 {
		t.Errorf("range, nothing to refuse: exit %d, want 0", got)
	}
}
