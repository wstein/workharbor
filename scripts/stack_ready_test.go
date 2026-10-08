package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// srRepo is a temp repo with three commits a<-b<-c on branch topic, a fake make
// on PATH that records its arguments, and a side commit outside the tip's history.
type srRepo struct {
	dir, bin, makeLog string
	a, b, c, side     string
}

func (r *srRepo) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // test helper
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newSRRepo(t *testing.T) *srRepo {
	t.Helper()
	root := t.TempDir()
	r := &srRepo{dir: filepath.Join(root, "repo"), bin: filepath.Join(root, "bin"), makeLog: filepath.Join(root, "make.log")}
	for _, d := range []string{r.dir, r.bin} {
		if err := os.MkdirAll(d, 0o755); err != nil { //nolint:gosec // test dirs
			t.Fatal(err)
		}
	}
	fake := "#!/bin/sh\necho \"$*\" >> '" + r.makeLog + "'\n"
	if err := os.WriteFile(filepath.Join(r.bin, "make"), []byte(fake), 0o755); err != nil { //nolint:gosec // fake executable
		t.Fatal(err)
	}
	r.git(t, "init", "-q", "-b", "main")
	commit := func(n string) string {
		r.git(t, "commit", "-q", "--allow-empty", "-m", n)
		return r.git(t, "rev-parse", "HEAD")
	}
	r.git(t, "checkout", "-q", "-b", "topic")
	r.a, r.b, r.c = commit("a"), commit("b"), commit("c")
	r.git(t, "checkout", "-q", "-b", "other", r.a)
	r.side = commit("side")
	r.git(t, "checkout", "-q", "topic")
	return r
}

func (r *srRepo) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("stack-ready.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", append([]string{script}, args...)...) //nolint:gosec // test helper
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "PATH="+r.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *srRepo) note(t *testing.T, sha string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "notes", "--ref=review", "show", sha) //nolint:gosec // test helper
	cmd.Dir = r.dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func TestStackReadyAppendsOnceAndPrints(t *testing.T) {
	r := newSRRepo(t)
	args := []string{r.c, "--opus", r.c, r.b, "--sonnet", r.a}
	out, err := r.run(t, args...)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := r.note(t, r.c); got != "CLEAR "+r.c+" role=review model=opus\n" {
		t.Errorf("tip note = %q", got)
	}
	if got := r.note(t, r.a); got != "CLEAR "+r.a+" role=review model=sonnet\n" {
		t.Errorf("a note = %q", got)
	}
	if !strings.Contains(out, "- topic "+r.c+": make land SHA="+r.c[:7]) {
		t.Errorf("no READY entry:\n%s", out)
	}
	if log, _ := os.ReadFile(r.makeLog); string(log) != "land-preview SHA="+r.c+"\n" {
		t.Errorf("make log = %q", log)
	}
	// Idempotent: a second run adds nothing.
	if out, err = r.run(t, args...); err != nil {
		t.Fatalf("rerun: %v\n%s", err, out)
	}
	if got := r.note(t, r.c); strings.Count(got, "CLEAR") != 1 {
		t.Errorf("duplicated note: %q", got)
	}
}

func TestStackReadyRefusals(t *testing.T) {
	r := newSRRepo(t)
	tests := map[string][]string{
		"short tip":          {r.c[:7], "--opus", r.c},
		"short sha":          {r.c, "--opus", r.b[:7]},
		"uppercase sha":      {r.c, "--opus", strings.ToUpper(r.b)},
		"not an ancestor":    {r.c, "--opus", r.side},
		"missing object":     {r.c, "--opus", strings.Repeat("0", 40)},
		"sha before tier":    {r.c, r.b},
		"no shas":            {r.c, "--opus"},
		"unknown option":     {r.c, "--haiku", r.b},
		"one bad among good": {r.c, "--opus", r.b, "--sonnet", r.side},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := r.run(t, args...)
			if err == nil {
				t.Fatalf("expected refusal:\n%s", out)
			}
			for _, sha := range []string{r.a, r.b, r.c, r.side} {
				if n := r.note(t, sha); n != "" {
					t.Errorf("note written despite refusal: %q", n)
				}
			}
			if _, err := os.Stat(r.makeLog); err == nil {
				t.Error("make ran despite refusal")
			}
		})
	}
}
