package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ssEnv is a git fixture (old chain o1<-o2 and a rebased chain n1<-n2 with the
// same patches, plus a changed chain c1<-c2) and a stub gh that logs its argv.
type ssEnv struct {
	dir, bin, fx, log                  string
	oldBase, oldHead, newBase, newHead string
	chgHead                            string
}

func (e *ssEnv) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // test helper
	cmd.Dir = e.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *ssEnv) write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o755); err != nil { //nolint:gosec // fixtures and fake executable
		t.Fatal(err)
	}
}

func (e *ssEnv) commitFile(t *testing.T, name, body string) string {
	t.Helper()
	e.write(t, filepath.Join(e.dir, name), body)
	e.git(t, "add", name)
	e.git(t, "commit", "-q", "-m", "add "+name)
	return e.git(t, "rev-parse", "HEAD")
}

func newSSEnv(t *testing.T) *ssEnv {
	t.Helper()
	root := t.TempDir()
	e := &ssEnv{dir: filepath.Join(root, "repo"), bin: filepath.Join(root, "bin"), fx: filepath.Join(root, "fx"), log: filepath.Join(root, "gh.log")}
	for _, d := range []string{e.dir, e.bin, e.fx} {
		if err := os.MkdirAll(d, 0o755); err != nil { //nolint:gosec // test dirs
			t.Fatal(err)
		}
	}
	e.write(t, filepath.Join(e.bin, "gh"), `#!/bin/sh
echo "$*" >> "$GH_LOG"
case "$1 $2" in
"api user") echo tester ;;
"pr view") cat "$GH_FX/pr-$3" ;;
"run list") for a; do [ "$p" = --commit ] && c=$a; p=$a; done; cat "$GH_FX/run-$c" ;;
*) exit 0 ;;
esac
`)
	e.git(t, "init", "-q", "-b", "main")
	e.git(t, "commit", "-q", "--allow-empty", "-m", "root")
	e.oldBase = e.git(t, "rev-parse", "HEAD")
	e.commitFile(t, "a.txt", "a\n")
	e.oldHead = e.commitFile(t, "b.txt", "b\n")
	// Rebased: same patches on a new base.
	e.git(t, "checkout", "-q", "-b", "moved", e.oldBase)
	e.newBase = e.commitFile(t, "base.txt", "x\n")
	e.commitFile(t, "a.txt", "a\n")
	e.newHead = e.commitFile(t, "b.txt", "b\n")
	// Changed: second patch differs.
	e.git(t, "checkout", "-q", "-b", "changed", e.newBase)
	e.commitFile(t, "a.txt", "a\n")
	e.chgHead = e.commitFile(t, "b.txt", "different\n")
	return e
}

func (e *ssEnv) pr(t *testing.T, n, head, base string) {
	t.Helper()
	e.write(t, filepath.Join(e.fx, "pr-"+n), "OPEN\t"+head+"\t"+base+"\n")
}

func (e *ssEnv) run(t *testing.T, head, status string) {
	t.Helper()
	e.write(t, filepath.Join(e.fx, "run-"+head), "77\t"+status+"\n")
}

func (e *ssEnv) exec(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	script, err := filepath.Abs("stack-status.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", append([]string{script}, args...)...) //nolint:gosec // test helper
	cmd.Dir = e.dir
	cmd.Env = append(os.Environ(), "PATH="+e.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_LOG="+e.log, "GH_FX="+e.fx, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	logb, _ := os.ReadFile(e.log) //nolint:gosec // test path
	return string(out), string(logb), err
}

func (e *ssEnv) rng() string { return e.oldBase + ".." + e.oldHead }

func TestStackStatusDryRunPostsNothing(t *testing.T) {
	e := newSSEnv(t)
	e.pr(t, "5", e.newHead, e.newBase)
	e.run(t, e.newHead, "completed")
	out, log, err := e.exec(t, "--tier", "opus", "5")
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "running as tester") || !strings.Contains(out, "would post review/opus") {
		t.Fatalf("output: %s", out)
	}
	if strings.Contains(log, "statuses") || strings.Contains(log, "rerun") {
		t.Fatalf("dry run wrote: %s", log)
	}
}

func TestStackStatusBadArgs(t *testing.T) {
	e := newSSEnv(t)
	for _, args := range [][]string{
		{"5"},
		{"--tier", "haiku", "5"},
		{"--tier", "opus"},
		{"--tier", "opus", "5x"},
		{"--tier", "opus", "--reviewed", "5=abc..def", "5"},
		{"--tier", "opus", "--bogus", "5"},
	} {
		if out, _, err := e.exec(t, args...); err == nil {
			t.Fatalf("%v accepted: %s", args, out)
		}
	}
}

func TestStackStatusApplyUnchangedHeadPostsThenReruns(t *testing.T) {
	e := newSSEnv(t)
	e.pr(t, "5", e.oldHead, e.oldBase)
	e.run(t, e.oldHead, "completed")
	out, log, err := e.exec(t, "--tier", "sonnet", "--apply", "--reviewed", "5="+e.rng(), "5")
	if err != nil || !strings.Contains(out, "unchanged") {
		t.Fatalf("err=%v out=%s", err, out)
	}
	post := strings.Index(log, "api repos/wstein/workharbor/statuses/"+e.oldHead+" -f state=success -f context=review/sonnet")
	rerun := strings.Index(log, "run rerun -R wstein/workharbor 77")
	if post < 0 || rerun < post {
		t.Fatalf("want post then rerun, log:\n%s", log)
	}
}

func TestStackStatusRangeDiffEqualPosts(t *testing.T) {
	e := newSSEnv(t)
	e.pr(t, "5", e.newHead, e.newBase)
	e.run(t, e.newHead, "completed")
	out, log, err := e.exec(t, "--tier", "opus", "--apply", "--reviewed", "5="+e.rng(), "5")
	if err != nil || !strings.Contains(out, "all '='") || !strings.Contains(log, "statuses/"+e.newHead) {
		t.Fatalf("err=%v out=%s log=%s", err, out, log)
	}
}

func TestStackStatusDifferingPatchRefuses(t *testing.T) {
	e := newSSEnv(t)
	e.pr(t, "5", e.chgHead, e.newBase)
	e.run(t, e.chgHead, "completed")
	out, log, err := e.exec(t, "--tier", "opus", "--apply", "--reviewed", "5="+e.rng(), "5")
	if err == nil || !strings.Contains(out, "needs a new review") || strings.Contains(log, "statuses") {
		t.Fatalf("err=%v out=%s log=%s", err, out, log)
	}
}

func TestStackStatusRefusesBusyGateRun(t *testing.T) {
	for _, st := range []string{"in_progress", "queued"} {
		e := newSSEnv(t)
		e.pr(t, "5", e.oldHead, e.oldBase)
		e.run(t, e.oldHead, st)
		out, log, err := e.exec(t, "--tier", "opus", "--apply", "5")
		if err == nil || !strings.Contains(out, st) || strings.Contains(log, "statuses") || strings.Contains(log, "rerun") {
			t.Fatalf("%s: err=%v out=%s log=%s", st, err, out, log)
		}
	}
}

func TestStackStatusOneBadPRPostsNothing(t *testing.T) {
	e := newSSEnv(t)
	e.pr(t, "5", e.oldHead, e.oldBase)
	e.run(t, e.oldHead, "completed")
	e.pr(t, "6", e.chgHead, e.newBase)
	e.run(t, e.chgHead, "completed")
	_, log, err := e.exec(t, "--tier", "opus", "--apply", "--reviewed", "6="+e.rng(), "5", "6")
	if err == nil || strings.Contains(log, "statuses") {
		t.Fatalf("err=%v log=%s", err, log)
	}
}
