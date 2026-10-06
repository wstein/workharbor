package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

// Exercise the actual land recipe with cheap check targets in isolated repositories.
// A fast-forward can introduce a merge commit even though it creates none itself.
func TestLinearLand(t *testing.T) {
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	_, recipe, ok := strings.Cut(string(makefile), "\nland:\n")
	if !ok {
		t.Fatal("land target missing")
	}
	recipe, _, _ = strings.Cut(recipe, "\n\n")
	indexScript, err := os.ReadFile("index-state.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		merge      bool
		historical bool
		move       bool
		mergeLater bool
		template   bool
		failScan   bool
	}{
		{name: "linear descendant"},
		{name: "affected template", template: true},
		{name: "secret scan failure", failScan: true},
		{name: "introduced merge", merge: true},
		{name: "historical merge", historical: true},
		{name: "topic moves during checks", move: true},
		{name: "topic gains merge during checks", move: true, mergeLater: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			home := t.TempDir()
			git := func(at string, args ...string) string {
				t.Helper()
				cmd := gittest.Git(t.Context(), home, at, gittest.Identity, args...)
				cmd.Env = gittest.Env(home, append(gittest.Identity,
					"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=")...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			write := func(path string, content []byte) {
				t.Helper()
				if err := os.WriteFile(path, content, 0o600); err != nil { //nolint:gosec // fixed files in an isolated test repository
					t.Fatal(err)
				}
			}
			git(dir, "init", "-q", "-b", "main")
			checks := "\n\n.PHONY: check check-ci check-local commitlint secrets-range check-generated\n" +
				"check check-ci:\n\t@echo forbidden-full-suite >&2; exit 1\n" +
				"check-local commitlint check-generated:\n"
			if tc.move {
				mutation := "git commit --allow-empty -qm moved"
				if tc.mergeLater {
					mutation = "git merge --no-ff -qm moved right"
				}
				checks += "\t@if [ ! -f checks-ran ]; then " + mutation + "; fi\n"
			}
			checks += "\t@echo $@ >> checks-ran\nsecrets-range:\n\t@echo secrets-range $(RANGE) $(TIP) >> checks-ran\n"
			if tc.failScan {
				checks += "\t@echo required-secret-scan-failed >&2; exit 1\n"
			}
			write(filepath.Join(dir, "Makefile"), []byte("LAND_MAKE := $(MAKE)\nland:\n"+recipe+checks))
			if err := os.Mkdir(filepath.Join(dir, "scripts"), 0o700); err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(dir, "scripts", "index-state.sh"), indexScript)
			if err := os.Chmod(filepath.Join(dir, "scripts", "index-state.sh"), 0o700); err != nil { //nolint:gosec // the copied test script must be executable
				t.Fatal(err)
			}
			git(dir, "add", ".")
			git(dir, "commit", "-qm", "base")
			merge := func(at string) {
				t.Helper()
				base := git(at, "rev-parse", "HEAD")
				git(at, "commit", "--allow-empty", "-qm", "left")
				git(at, "switch", "-qc", "right", base)
				git(at, "commit", "--allow-empty", "-qm", "right")
				git(at, "switch", "-q", "-")
				git(at, "merge", "--no-ff", "-qm", "merge", "right")
			}
			if tc.historical {
				merge(dir)
			}
			base := git(dir, "rev-parse", "main")
			topic := filepath.Join(t.TempDir(), "topic")
			git(dir, "worktree", "add", "-qb", "topic", topic)
			if tc.merge {
				merge(topic)
			} else {
				if tc.template {
					if err := os.MkdirAll(filepath.Join(topic, "internal", "web"), 0o700); err != nil {
						t.Fatal(err)
					}
					write(filepath.Join(topic, "internal", "web", "fixture.templ"), []byte("template fixture\n"))
					git(topic, "add", ".")
				}
				git(topic, "commit", "--allow-empty", "-qm", "linear")
			}
			if tc.mergeLater {
				git(topic, "switch", "-qc", "right", base)
				git(topic, "commit", "--allow-empty", "-qm", "right")
				git(topic, "switch", "-q", "topic")
			}
			candidate := git(topic, "rev-parse", "HEAD")
			// Demonstrate that the old ancestry and ff-only safeguards accept this graph.
			git(topic, "merge-base", "--is-ancestor", base, "HEAD")
			if tc.merge {
				git(topic, "branch", "ff-proof", base)
				proof := filepath.Join(t.TempDir(), "proof")
				git(topic, "worktree", "add", "-q", proof, "ff-proof")
				git(proof, "merge", "--ff-only", "-q", candidate)
				if got := git(proof, "rev-parse", "HEAD"); got != candidate {
					t.Fatalf("ff-only proof ended at %s, want %s", got, candidate)
				}
			}
			cmd := exec.CommandContext(t.Context(), "make", "-s", "land")
			cmd.Dir = topic
			cmd.Env = gittest.Env(home, append(gittest.Identity,
				"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=")...)
			out, err := cmd.CombinedOutput()
			if tc.merge {
				if err == nil || !strings.Contains(string(out), "introduces merge commits") {
					t.Fatalf("want merge rejection, got %v\n%s", err, out)
				}
				if _, err := os.Stat(filepath.Join(topic, "checks-ran")); !os.IsNotExist(err) {
					t.Fatalf("checks ran before merge rejection: %v", err)
				}
				if got := git(dir, "rev-parse", "main"); got != base {
					t.Fatalf("rejected candidate changed main to %s", got)
				}
			} else if tc.failScan {
				if err == nil || !strings.Contains(string(out), "required-secret-scan-failed") {
					t.Fatalf("want secret failure, got %v\n%s", err, out)
				}
				if got := git(dir, "rev-parse", "main"); got != base {
					t.Fatalf("failed scan changed main to %s", got)
				}
			} else if tc.move {
				if got := git(topic, "rev-parse", "HEAD"); got == candidate {
					t.Fatal("checker did not move the topic")
				}
				if err == nil || !strings.Contains(string(out), "candidate moved during the checks") {
					t.Fatalf("want candidate movement rejection, got %v\n%s", err, out)
				}
				if got := git(dir, "rev-parse", "main"); got != base {
					t.Fatalf("moved candidate changed main to %s", got)
				}
			} else {
				if err != nil {
					t.Fatalf("linear landing: %v\n%s", err, out)
				}
				if got := git(dir, "rev-parse", "main"); got != candidate {
					t.Fatalf("main = %s, want %s", got, candidate)
				}
				logged, err := os.ReadFile(filepath.Join(topic, "checks-ran")) //nolint:gosec // fixed gate log in an isolated test repository
				if err != nil {
					t.Fatal(err)
				}
				want := "check-local\ncommitlint\nsecrets-range " + base + ".." + candidate + " " + candidate + "\n"
				if tc.template {
					want += "check-generated\n"
				}
				if string(logged) != want {
					t.Fatalf("local gates = %q, want %q", logged, want)
				}
			}
		})
	}
}

// landBranchRepo is an isolated shared checkout on main with the real land
// recipe and cheap check targets, for exercising make land BRANCH=<name>.
type landBranchRepo struct {
	t      *testing.T
	dir    string
	home   string
	recipe string
}

func newLandBranchRepo(t *testing.T, moveMain bool) *landBranchRepo {
	t.Helper()
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	_, recipe, ok := strings.Cut(string(makefile), "\nland:\n")
	if !ok {
		t.Fatal("land target missing")
	}
	recipe, _, _ = strings.Cut(recipe, "\n\n")
	indexScript, err := os.ReadFile("index-state.sh")
	if err != nil {
		t.Fatal(err)
	}
	r := &landBranchRepo{t: t, dir: t.TempDir(), home: t.TempDir(), recipe: recipe}
	r.git(r.dir, "init", "-q", "-b", "main")
	checks := "\n\n.PHONY: check-local commitlint secrets-range check-generated\n" +
		"check-local commitlint check-generated:\n"
	if moveMain {
		checks += "\t@if [ ! -f checks-ran ]; then git update-ref refs/heads/main \"$$(git commit-tree -p main -m moved main^{tree})\"; fi\n"
	}
	checks += "\t@echo $@ >> checks-ran\nsecrets-range:\n\t@echo secrets-range $(RANGE) $(TIP) >> checks-ran\n"
	r.write(filepath.Join(r.dir, "Makefile"), "LAND_MAKE := $(MAKE)\nland:\n"+recipe+checks)
	if err := os.Mkdir(filepath.Join(r.dir, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	r.write(filepath.Join(r.dir, "scripts", "index-state.sh"), string(indexScript))
	if err := os.Chmod(filepath.Join(r.dir, "scripts", "index-state.sh"), 0o700); err != nil { //nolint:gosec // the copied test script must be executable
		t.Fatal(err)
	}
	r.write(filepath.Join(r.dir, "tracked.txt"), "tracked\n")
	r.git(r.dir, "add", ".")
	r.git(r.dir, "commit", "-qm", "base")
	return r
}

func (r *landBranchRepo) env() []string {
	return gittest.Env(r.home, append(gittest.Identity,
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=")...)
}

func (r *landBranchRepo) git(at string, args ...string) string {
	r.t.Helper()
	cmd := gittest.Git(r.t.Context(), r.home, at, gittest.Identity, args...)
	cmd.Env = r.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *landBranchRepo) write(path, content string) {
	r.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // fixed files in an isolated test repository
		r.t.Fatal(err)
	}
}

// topic adds a worktree on a new branch with one commit and returns its path.
func (r *landBranchRepo) topic(branch string) string {
	r.t.Helper()
	wt := filepath.Join(r.t.TempDir(), "wt")
	r.git(r.dir, "worktree", "add", "-q", "-b", branch, wt)
	r.git(wt, "commit", "--allow-empty", "-qm", "linear "+branch)
	return wt
}

func (r *landBranchRepo) land(at string, extraEnv []string, args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.CommandContext(r.t.Context(), "make", append([]string{"-s", "land"}, args...)...) //nolint:gosec // fixed make target, test-controlled arguments, isolated repository
	cmd.Dir = at
	cmd.Env = append(r.env(), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *landBranchRepo) wantRefused(at string, msg string, args ...string) {
	r.t.Helper()
	base := r.git(r.dir, "rev-parse", "main")
	out, err := r.land(at, nil, args...)
	if err == nil || !strings.Contains(out, msg) {
		r.t.Fatalf("want refusal containing %q, got %v\n%s", msg, err, out)
	}
	if got := r.git(r.dir, "rev-parse", "main"); got != base {
		r.t.Fatalf("refused land moved main to %s", got)
	}
}

func TestLandBranchArg(t *testing.T) {
	t.Run("lands from the shared checkout without switching", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		candidate := r.git(wt, "rev-parse", "HEAD")
		base := r.git(r.dir, "rev-parse", "main")
		out, err := r.land(r.dir, nil, "BRANCH=topic")
		if err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); got != candidate {
			t.Fatalf("main = %s, want %s", got, candidate)
		}
		if got := r.git(r.dir, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
			t.Fatalf("shared checkout HEAD = %s", got)
		}
		if got := r.git(wt, "symbolic-ref", "--short", "HEAD"); got != "topic" {
			t.Fatalf("worktree HEAD = %s", got)
		}
		logged, err := os.ReadFile(filepath.Join(wt, "checks-ran")) //nolint:gosec // fixed gate log in an isolated test repository
		if err != nil {
			t.Fatalf("checks did not run in the branch's worktree: %v", err)
		}
		if want := "check-local\ncommitlint\nsecrets-range " + base + ".." + candidate + " " + candidate + "\n"; string(logged) != want {
			t.Fatalf("local gates = %q, want %q", logged, want)
		}
		if _, err := os.Stat(filepath.Join(r.dir, "checks-ran")); !os.IsNotExist(err) {
			t.Fatalf("checks ran in the shared checkout: %v", err)
		}
	})
	t.Run("odd branch names are data, not shell", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		name := "a;touch&pwned|`pwned2`'\"$(pwned3)${IFS}"
		wt := r.topic(name)
		out, err := r.land(r.dir, nil, "BRANCH="+strings.ReplaceAll(name, "$", "$$")) // make: $$ is a literal dollar
		if err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
		for _, f := range []string{"pwned", "pwned2", "pwned3"} {
			for _, d := range []string{r.dir, wt} {
				if _, err := os.Stat(filepath.Join(d, f)); !os.IsNotExist(err) {
					t.Fatalf("injection created %s in %s", f, d)
				}
			}
		}
	})
	t.Run("a worktree path with a newline", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := filepath.Join(t.TempDir(), "wt\nbranch refs/heads/other")
		r.git(r.dir, "worktree", "add", "-q", "-b", "topic", wt)
		r.git(wt, "commit", "--allow-empty", "-qm", "linear")
		if out, err := r.land(r.dir, nil, "BRANCH=topic"); err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
	})
	t.Run("an exported BRANCH does not select a worktree", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		candidate := r.git(wt, "rev-parse", "HEAD")
		out, err := r.land(wt, []string{"BRANCH=nonexistent", "SHA=bad"})
		if err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); got != candidate {
			t.Fatalf("main = %s", got)
		}
	})
	t.Run("a prefix of a branch name is not the branch", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		r.wantRefused(r.dir, "no worktree has top checked out", "BRANCH=top")
	})
	t.Run("missing branch", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		r.wantRefused(r.dir, "no worktree has nope checked out", "BRANCH=nope")
	})
	t.Run("branch without a worktree", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		r.git(wt, "switch", "-q", "--detach")
		r.wantRefused(r.dir, "no worktree has topic checked out", "BRANCH=topic")
	})
	t.Run("branch checked out twice", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		r.git(r.dir, "worktree", "add", "-qf", filepath.Join(t.TempDir(), "again"), "topic")
		r.wantRefused(r.dir, "more than one worktree", "BRANCH=topic")
	})
	t.Run("prunable worktree", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		if err := os.RemoveAll(wt); err != nil {
			t.Fatal(err)
		}
		r.wantRefused(r.dir, "prunable", "BRANCH=topic")
	})
	t.Run("branch not on top of main", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		r.git(r.dir, "commit", "--allow-empty", "-qm", "main moved")
		r.wantRefused(r.dir, "not on top of main", "BRANCH=topic")
	})
	t.Run("introduced merge commits", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		r.git(wt, "switch", "-qc", "side", "main")
		r.git(wt, "commit", "--allow-empty", "-qm", "side")
		r.git(wt, "switch", "-q", "topic")
		r.git(wt, "merge", "--no-ff", "-qm", "merge", "side")
		r.wantRefused(r.dir, "introduces merge commits", "BRANCH=topic")
	})
	t.Run("shared checkout not on main", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		r.git(r.dir, "switch", "-q", "--detach")
		out, err := r.land(r.dir, nil, "BRANCH=topic")
		if err == nil || !strings.Contains(out, "is not on main") {
			t.Fatalf("want refusal, got %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "HEAD" {
			t.Fatalf("shared checkout was switched to %s", got)
		}
	})
	t.Run("stale shared index", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		r.write(filepath.Join(r.dir, "tracked.txt"), "changed\n")
		r.git(r.dir, "add", "tracked.txt")
		r.write(filepath.Join(r.dir, "tracked.txt"), "tracked\n")
		r.wantRefused(r.dir, "index is stale", "BRANCH=topic")
	})
	t.Run("main moved during the checks", func(t *testing.T) {
		r := newLandBranchRepo(t, true)
		wt := r.topic("topic")
		base := r.git(r.dir, "rev-parse", "main")
		out, err := r.land(r.dir, nil, "BRANCH=topic")
		if err == nil || !strings.Contains(out, "main moved during the checks") {
			t.Fatalf("want refusal, got %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); got == base || got == r.git(wt, "rev-parse", "HEAD") {
			t.Fatalf("main = %s: want it moved by the check and not landed", got)
		}
	})
	t.Run("never lands main or odd names", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		for _, tc := range []struct{ arg, msg string }{
			{"BRANCH=main", "never land main"},
			{"BRANCH=", "BRANCH is empty"},
			{"BRANCH=--help", "must not start with a dash"},
			{"BRANCH=-topic", "must not start with a dash"},
			{"BRANCH=a..b", "not a valid branch name"},
			{"BRANCH=refs/heads/topic", "no worktree has refs/heads/topic"},
			{"BRANCH=top ic", "not a valid branch name"},
		} {
			r.wantRefused(r.dir, tc.msg, tc.arg)
		}
	})
	t.Run("bare repository", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		bare := filepath.Join(t.TempDir(), "bare.git")
		r.git(r.dir, "clone", "-q", "--bare", r.dir, bare)
		r.write(filepath.Join(bare, "Makefile"), "LAND_MAKE := $(MAKE)\nland:\n"+r.recipe)
		out, err := r.land(bare, nil, "BRANCH=topic")
		if err == nil || !strings.Contains(out, "not a bare repository") {
			t.Fatalf("want refusal, got %v\n%s", err, out)
		}
	})
}

func TestLandSHAArg(t *testing.T) {
	r := newLandBranchRepo(t, false)
	wt := r.topic("topic")
	candidate := r.git(wt, "rev-parse", "HEAD")
	other := strings.Repeat("a", 40)
	for _, tc := range []struct{ sha, msg string }{
		{other, "not the requested SHA"},
		{candidate[:12], "full 40-character"},
		{strings.ToUpper(candidate), "full 40-character"},
		{"", "full 40-character"},
		{candidate + "0", "full 40-character"},
		{"HEAD", "full 40-character"},
	} {
		r.wantRefused(r.dir, tc.msg, "BRANCH=topic", "SHA="+tc.sha)
		r.wantRefused(wt, tc.msg, "SHA="+tc.sha)
	}
	if out, err := r.land(r.dir, nil, "BRANCH=topic", "SHA="+candidate); err != nil {
		t.Fatalf("land: %v\n%s", err, out)
	}
	if got := r.git(r.dir, "rev-parse", "main"); got != candidate {
		t.Fatalf("main = %s, want %s", got, candidate)
	}
}

// make -n, -t, -q and MAKEFLAGS only print a recipe line that contains $(MAKE),
// and the land recipe is one such line: its guards and the merge must never run
// for real without the checks.
func TestLandIgnoresDryRunFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		args []string
	}{
		{"-n branch", nil, []string{"-n", "BRANCH=topic"}},
		{"-n plain", nil, []string{"-n"}},
		{"--dry-run", nil, []string{"--dry-run", "BRANCH=topic"}},
		{"--just-print", nil, []string{"--just-print", "BRANCH=topic"}},
		{"-t", nil, []string{"-t", "BRANCH=topic"}},
		{"--touch", nil, []string{"--touch", "BRANCH=topic"}},
		{"-q", nil, []string{"-q", "BRANCH=topic"}},
		{"--question", nil, []string{"--question", "BRANCH=topic"}},
		{"MAKEFLAGS=n", []string{"MAKEFLAGS=n"}, []string{"BRANCH=topic"}},
		{"GNUMAKEFLAGS=-n", []string{"GNUMAKEFLAGS=-n"}, []string{"BRANCH=topic"}},
		{"GNUMAKEFLAGS=-n plain", []string{"GNUMAKEFLAGS=-n"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.env) > 0 && strings.HasPrefix(tc.env[0], "GNUMAKEFLAGS") {
				// GNU make 3.81 (macOS) ignores GNUMAKEFLAGS and would really land.
				if out, err := exec.CommandContext(t.Context(), "make", "--version").Output(); err != nil || strings.Contains(string(out), "GNU Make 3.") {
					t.Skip("make does not support GNUMAKEFLAGS")
				}
			}
			r := newLandBranchRepo(t, false)
			wt := r.topic("topic")
			base := r.git(r.dir, "rev-parse", "main")
			at := r.dir
			if tc.args[len(tc.args)-1] != "BRANCH=topic" {
				at = wt
			}
			cmd := exec.CommandContext(t.Context(), "make", append([]string{"-s", "land"}, tc.args...)...) //nolint:gosec // fixed make target, test-controlled arguments, isolated repository
			cmd.Dir = at
			cmd.Env = append(r.env(), tc.env...)
			out, _ := cmd.CombinedOutput()
			if got := r.git(r.dir, "rev-parse", "main"); got != base {
				t.Fatalf("main moved to %s under %s without the checks\n%s", got, tc.name, out)
			}
			if _, err := os.Stat(filepath.Join(wt, "checks-ran")); !os.IsNotExist(err) {
				t.Fatalf("checks marker present: %v", err)
			}
		})
	}
}
