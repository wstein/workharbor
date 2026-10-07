package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newLandQueueRepo(t *testing.T) *landBranchRepo {
	t.Helper()
	r := newLandBranchRepo(t, false)
	source, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	_, recipe, ok := strings.Cut(string(source), "\nland-list land-next land-all land-preview:\n")
	if !ok {
		t.Fatal("queue targets missing")
	}
	recipe, _, _ = strings.Cut(recipe, "\n\n")
	path := filepath.Join(r.dir, "Makefile")
	current, err := os.ReadFile(path) //nolint:gosec // fixed Makefile in isolated test repository
	if err != nil {
		t.Fatal(err)
	}
	r.write(path, string(current)+"\nland-list land-next land-all land-preview:\n"+recipe+"\n")
	r.git(r.dir, "commit", "-qam", "queue recipe")
	return r
}

func (r *landBranchRepo) queue(target string, args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.CommandContext(r.t.Context(), "make", append([]string{"-s", target}, args...)...) //nolint:gosec // fixed targets and arguments in isolated repositories
	cmd.Dir, cmd.Env = r.dir, r.env()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestLandQueueListAndPreview(t *testing.T) {
	r := newLandQueueRepo(t)
	wt := r.topic("z-unstamped")
	unstamped := r.git(wt, "rev-parse", "HEAD")
	wt = r.topic("a-stamped")
	sha := r.git(wt, "rev-parse", "HEAD")
	r.stamp(sha, "Reviewed by wh/review", sha)
	r.git(r.dir, "branch", "alias", sha)
	base := r.git(r.dir, "rev-parse", "main")
	notes := r.git(r.dir, "rev-parse", "refs/notes/review")
	before := r.git(r.dir, "worktree", "list", "--porcelain")
	index, err := os.ReadFile(filepath.Join(r.dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.queue("land-list")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{sha, unstamped, "review stamp: matched", "review stamp: mismatch", "(no review note)", "path class: ordinary"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "branch    a-stamped") > strings.Index(out, "branch    z-unstamped") {
		t.Fatal("queue order is not lexical")
	}
	r.git(r.dir, "branch", "-D", "alias")
	out, err = r.queue("land-preview", "SHA="+sha[:9])
	if err != nil || !strings.Contains(out, sha) || strings.Contains(out, "[y/N]") {
		t.Fatalf("preview: %v\n%s", err, out)
	}
	if r.git(r.dir, "rev-parse", "main") != base || r.git(r.dir, "rev-parse", "refs/notes/review") != notes || r.git(r.dir, "worktree", "list", "--porcelain") != before {
		t.Fatal("list/preview mutated refs or worktrees")
	}
	after, err := os.ReadFile(filepath.Join(r.dir, ".git", "index"))
	if err != nil || string(after) != string(index) {
		t.Fatal("list/preview wrote index")
	}
}

func TestLandQueueStopsAtMismatch(t *testing.T) {
	for _, target := range []string{"land-next", "land-all"} {
		t.Run(target, func(t *testing.T) {
			r := newLandQueueRepo(t)
			wt := r.topic("a-stale")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "Reviewed by wh/review", strings.Repeat("a", 40))
			wt = r.topic("b-valid")
			second := r.git(wt, "rev-parse", "HEAD")
			r.stamp(second, "Reviewed by wh/review", second)
			base := r.git(r.dir, "rev-parse", "main")
			out, err := r.queue(target)
			if err == nil || !strings.Contains(out, "review note is not for") || strings.Contains(out, "candidate "+second) {
				t.Fatalf("mismatch did not stop: %v\n%s", err, out)
			}
			if r.git(r.dir, "rev-parse", "main") != base {
				t.Fatal("mismatch moved main")
			}
		})
	}
}

func TestLandQueueUsesConfirmation(t *testing.T) {
	r := newLandQueueRepo(t)
	r.topic("a-unstamped")
	wt := r.topic("b-stamped")
	sha := r.git(wt, "rev-parse", "HEAD")
	r.stamp(sha, "review", sha)
	base := r.git(r.dir, "rev-parse", "main")
	cmd := r.ttyCmd(r.dir, t.TempDir(), nil)
	for i, arg := range cmd.Args {
		cmd.Args[i] = strings.ReplaceAll(arg, "make -s land", "make -s land-next")
	}
	out, err := r.runTTY(cmd, "n\n")
	if err == nil || !strings.Contains(out, sha) || !strings.Contains(out, "not landing") {
		t.Fatalf("queue confirm: %v\n%s", err, out)
	}
	if r.git(r.dir, "rev-parse", "main") != base {
		t.Fatal("cancel moved main")
	}
}

func TestLandQueueSequentialLanding(t *testing.T) {
	for _, target := range []string{"land-next", "land-all"} {
		t.Run(target, func(t *testing.T) {
			r := newLandQueueRepo(t)
			wt := r.topic("a-first")
			first := r.git(wt, "rev-parse", "HEAD")
			r.stamp(first, "first review", first)
			r.git(wt, "switch", "-qc", "b-second")
			r.git(wt, "commit", "--allow-empty", "-qm", "second")
			second := r.git(wt, "rev-parse", "HEAD")
			r.stamp(second, "second review", second)
			tmp := t.TempDir()
			cmd := r.ttyCmd(r.dir, tmp, nil)
			for i, arg := range cmd.Args {
				cmd.Args[i] = strings.ReplaceAll(arg, "make -s land", "make -s "+target)
			}
			out, err := r.runTTY(cmd, "y\ny\n")
			if err != nil {
				t.Fatalf("queue: %v\n%s", err, out)
			}
			want := first
			if target == "land-all" {
				want = second
			}
			if got := r.git(r.dir, "rev-parse", "main"); got != want {
				t.Fatalf("main = %s, want %s\n%s", got, want, out)
			}
			if target == "land-all" && (!strings.Contains(out, "first review") || !strings.Contains(out, "second review")) {
				t.Fatal("missing per-tip reviews")
			}
			r.wantNoTemp(tmp)
		})
	}
}

func TestLandQueuePreviewCarveOut(t *testing.T) {
	r := newLandQueueRepo(t)
	wt := r.topic("security")
	r.write(filepath.Join(wt, "AGENTS.md"), "security-relevant\n")
	r.git(wt, "add", "AGENTS.md")
	r.git(wt, "commit", "-qm", "rule")
	sha := r.git(wt, "rev-parse", "HEAD")
	r.stamp(sha, "ordinary claimed by note", sha)
	out, err := r.queue("land-preview", "SHA="+sha)
	if err != nil || !strings.Contains(out, "path class: carve-out") || strings.Contains(out, "type "+sha[:7]) {
		t.Fatalf("preview: %v\n%s", err, out)
	}
}

func TestLandQueueDryRunAndCallerOverrides(t *testing.T) {
	for _, target := range []string{"land-next", "land-all"} {
		for _, arg := range []string{"-n", "-t", "-q", "MAKE=true", "MAKE_COMMAND=true", "MAKEFILES=missing.mk"} {
			t.Run(target+arg, func(t *testing.T) {
				r := newLandQueueRepo(t)
				wt := r.topic("topic")
				sha := r.git(wt, "rev-parse", "HEAD")
				r.stamp(sha, "review", sha)
				base := r.git(r.dir, "rev-parse", "main")
				out, _ := r.queue(target, arg)
				if r.git(r.dir, "rev-parse", "main") != base {
					t.Fatalf("caller flag moved main\n%s", out)
				}
				if _, err := os.Stat(filepath.Join(wt, "checks-ran")); !os.IsNotExist(err) {
					t.Fatalf("unexpected checks: %v\n%s", err, out)
				}
			})
		}
	}
}

func TestLandQueueBranchMovementStopsAll(t *testing.T) {
	r := newLandQueueRepo(t)
	path := filepath.Join(r.dir, "Makefile")
	data, err := os.ReadFile(path) //nolint:gosec // fixed Makefile in isolated test repository
	if err != nil {
		t.Fatal(err)
	}
	r.write(path, string(data)+"\ncheck-local:\n\t@git update-ref refs/heads/b-second \"$$(git commit-tree -p refs/heads/b-second -m moved refs/heads/b-second^{tree})\"\n")
	r.git(r.dir, "commit", "-qam", "move queued branch during first checks")
	wt := r.topic("a-first")
	first := r.git(wt, "rev-parse", "HEAD")
	r.stamp(first, "first review", first)
	r.git(wt, "switch", "-qc", "b-second")
	r.git(wt, "commit", "--allow-empty", "-qm", "second")
	second := r.git(wt, "rev-parse", "HEAD")
	r.stamp(second, "second review", second)
	cmd := r.ttyCmd(r.dir, t.TempDir(), nil)
	for i, arg := range cmd.Args {
		cmd.Args[i] = strings.ReplaceAll(arg, "make -s land", "make -s land-all")
	}
	out, err := r.runTTY(cmd, "y\n")
	if err == nil || !strings.Contains(out, "queue branch b-second moved") {
		t.Fatalf("movement not refused: %v\n%s", err, out)
	}
	if r.git(r.dir, "rev-parse", "main") != first {
		t.Fatal("queue landed beyond first candidate")
	}
}
