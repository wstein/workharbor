package scripts_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// stamp adds a review note for sha that names at (the sha it claims to review).
func (r *landBranchRepo) stamp(sha, text, at string) {
	r.t.Helper()
	r.git(r.dir, "notes", "--ref=review", "add", "-f", "-m", text+" at "+at, sha)
}

// detachedTopic is a branch with one commit that no worktree has checked out.
func (r *landBranchRepo) detachedTopic() (sha string) {
	r.t.Helper()
	wt := r.topic("topic")
	sha = r.git(wt, "rev-parse", "HEAD")
	r.git(r.dir, "worktree", "remove", "--force", wt)
	return sha
}

// landTTY runs make land with a pseudo terminal as stdin and stderr (script(1)),
// feeding input to it. tmp is the TMPDIR of the run.
func (r *landBranchRepo) landTTY(at, input, tmp string, extraEnv []string, args ...string) (string, error) {
	r.t.Helper()
	return r.runTTY(r.ttyCmd(at, tmp, extraEnv, args...), input)
}

// syncBuf is an output buffer that the test reads while the command writes it.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// runTTY starts cmd and types input once land has printed its prompt, as a
// person would; input typed earlier is lost to the terminal mode switches.
func (r *landBranchRepo) runTTY(cmd *exec.Cmd, input string) (string, error) {
	r.t.Helper()
	in, err := cmd.StdinPipe()
	if err != nil {
		r.t.Fatal(err)
	}
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.After(60 * time.Second)
	typed := false
	for !typed {
		select {
		case err := <-done:
			_ = in.Close()
			return out.String(), err
		case <-deadline:
			_ = cmd.Process.Kill()
			r.t.Fatalf("land never prompted\n%s", out.String())
		case <-time.After(20 * time.Millisecond):
			if s := out.String(); strings.Contains(s, "[y/N] ") || strings.Contains(s, "to land it: ") {
				time.Sleep(100 * time.Millisecond)
				_, _ = in.Write([]byte(input))
				typed = true
			}
		}
	}
	err = <-done
	_ = in.Close()
	return out.String(), err
}

func (r *landBranchRepo) ttyCmd(at, tmp string, extraEnv []string, args ...string) *exec.Cmd {
	r.t.Helper()
	if _, err := exec.LookPath("script"); err != nil {
		if os.Getenv("CI") != "" {
			r.t.Fatal("script(1) is required in CI for the pseudo terminal tests")
		}
		r.t.Skip("script(1) not available for a pseudo terminal")
	}
	line := "make -s land"
	for _, a := range args {
		line += " '" + a + "'"
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(r.t.Context(), "script", "-q", "/dev/null", "sh", "-c", line) //nolint:gosec // test-controlled arguments, isolated repository
	} else {
		cmd = exec.CommandContext(r.t.Context(), "script", "-qec", line, "/dev/null") //nolint:gosec // test-controlled arguments, isolated repository
	}
	cmd.Dir = at
	cmd.Env = append(r.env(), "TMPDIR="+tmp)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

func (r *landBranchRepo) wantTTYRefused(at, input, msg string, extraEnv []string, args ...string) {
	r.t.Helper()
	base := r.git(r.dir, "rev-parse", "main")
	tmp := r.t.TempDir()
	out, err := r.landTTY(at, input, tmp, extraEnv, args...)
	if err == nil || !strings.Contains(out, msg) {
		r.t.Fatalf("want refusal containing %q, got %v\n%s", msg, err, out)
	}
	if got := r.git(r.dir, "rev-parse", "main"); got != base {
		r.t.Fatalf("refused land moved main to %s", got)
	}
	r.wantNoTemp(tmp)
}

// wantNoTemp checks that the temporary worktree is gone from disk and from git.
func (r *landBranchRepo) wantNoTemp(tmp string) {
	r.t.Helper()
	left, _ := os.ReadDir(tmp)
	if len(left) != 0 {
		r.t.Fatalf("temporary worktree left behind in %s: %v", tmp, left)
	}
	if list := r.git(r.dir, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1+strings.Count(list, "branch refs/heads/")-strings.Count(list, "branch refs/heads/main") {
		// only the shared checkout and live topic worktrees may be listed; a /land. path is a leak
		if strings.Contains(list, "/land.") {
			r.t.Fatalf("temporary worktree still registered:\n%s", list)
		}
	}
}

func TestLandShortSHA(t *testing.T) {
	t.Run("lands a branch with no worktree from the shared checkout", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.stamp(sha, "Reviewed by wh/review", sha)
		tmp := t.TempDir()
		out, err := r.landTTY(r.dir, "y\n", tmp, nil, "SHA="+sha[:9])
		if err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
		for _, want := range []string{sha, "Reviewed by wh/review", "path class: ordinary", "land: main is now"} {
			if !strings.Contains(out, want) {
				t.Fatalf("output lacks %q:\n%s", want, out)
			}
		}
		if got := r.git(r.dir, "rev-parse", "main"); got != sha {
			t.Fatalf("main = %s, want %s", got, sha)
		}
		r.wantNoTemp(tmp)
	})
	t.Run("lands from a detached worktree", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.stamp(sha, "ok", sha)
		det := filepath.Join(t.TempDir(), "det")
		r.git(r.dir, "worktree", "add", "-q", "--detach", det, "main")
		out, err := r.landTTY(det, "y\n", t.TempDir(), nil, "SHA="+sha[:7])
		if err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); got != sha {
			t.Fatalf("main = %s, want %s", got, sha)
		}
	})
	t.Run("uses the worktree that has the branch checked out", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		sha := r.git(wt, "rev-parse", "HEAD")
		r.stamp(sha, "ok", sha)
		tmp := t.TempDir()
		if out, err := r.landTTY(r.dir, "y\n", tmp, nil, "SHA="+sha[:8]); err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); got != sha {
			t.Fatalf("main = %s, want %s", got, sha)
		}
		if _, err := os.Stat(filepath.Join(wt, "checks-ran")); err != nil {
			t.Fatalf("the checks did not run in the topic worktree: %v", err)
		}
	})
	t.Run("input refusals", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.stamp(sha, "ok", sha)
		for _, tc := range []struct{ sha, msg string }{
			{sha[:6], "7 to 40"},
			{"", "7 to 40"},
			{strings.ToUpper(sha[:9]), "7 to 40"},
			{"topic", "7 to 40"},
			{"HEAD", "7 to 40"},
			{"abcdef1", "unknown SHA"},
		} {
			r.wantRefused(r.dir, tc.msg, "SHA="+tc.sha)
		}
		r.wantRefused(r.dir, "with BRANCH= the SHA must be the full", "BRANCH=topic", "SHA="+sha[:9])
	})
	t.Run("non-terminal and answers", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.stamp(sha, "ok", sha)
		r.wantRefused(r.dir, "not a terminal", "SHA="+sha[:9])
		r.wantTTYRefused(r.dir, "n\n", "not landing", nil, "SHA="+sha[:9])
		r.wantTTYRefused(r.dir, "\n", "not landing", nil, "SHA="+sha[:9])
		r.wantTTYRefused(r.dir, "yes please\n", "not landing", nil, "SHA="+sha[:9])
	})
	t.Run("note must name the tip", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.wantTTYRefused(r.dir, "y\n", "has no review note", nil, "SHA="+sha[:9])
		r.stamp(sha, "Reviewed by wh/review", strings.Repeat("a", 40))
		r.wantTTYRefused(r.dir, "y\n", "review note is not for", nil, "SHA="+sha[:9])
		r.git(r.dir, "notes", "--ref=review", "add", "-f", "-m", "no sha here", sha)
		r.wantTTYRefused(r.dir, "y\n", "review note is not for", nil, "SHA="+sha[:9])
		r.git(r.dir, "notes", "--ref=review", "add", "-f", "-m", "at "+sha+" and at "+strings.Repeat("b", 40), sha)
		r.wantTTYRefused(r.dir, "y\n", "review note is not for", nil, "SHA="+sha[:9])
	})
	t.Run("not a tip, no branch, several branches", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		first := r.git(wt, "rev-parse", "HEAD")
		r.git(wt, "commit", "--allow-empty", "-qm", "second")
		r.git(r.dir, "worktree", "remove", "--force", wt)
		r.wantRefused(r.dir, "not the tip of any local branch", "SHA="+first[:9])
		r.git(r.dir, "branch", "-q", "-f", "orphanbase", "main")
		loose := r.git(r.dir, "commit-tree", "-p", "main", "-m", "loose", "main^{tree}")
		r.wantRefused(r.dir, "no local branch has", "SHA="+loose[:9])
		tip := r.git(r.dir, "rev-parse", "topic")
		r.git(r.dir, "branch", "-q", "twin", tip)
		r.stamp(tip, "ok", tip)
		out, err := r.land(r.dir, nil, "SHA="+tip[:9])
		if err == nil || !strings.Contains(out, "several branches") || !strings.Contains(out, "topic") || !strings.Contains(out, "twin") {
			t.Fatalf("want candidate list, got %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); strings.HasPrefix(got, tip) {
			t.Fatal("landed an ambiguous branch")
		}
	})
	t.Run("main itself", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		m := r.git(r.dir, "rev-parse", "main")
		r.wantRefused(r.dir, "main itself", "SHA="+m[:9])
	})
	t.Run("ambiguous prefix", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		var stream bytes.Buffer
		stream.WriteString("reset refs/heads/pile\nfrom refs/heads/main\n")
		for i := range 90000 {
			msg := fmt.Sprintf("c%d\n", i)
			fmt.Fprintf(&stream, "commit refs/heads/pile\ncommitter a <a@example.com> %d +0000\ndata %d\n%s", 1700000000+i, len(msg), msg)
		}
		imp := exec.CommandContext(t.Context(), "git", "fast-import", "--quiet")
		imp.Dir = r.dir
		imp.Env = r.env()
		imp.Stdin = &stream
		if out, err := imp.CombinedOutput(); err != nil {
			t.Fatalf("fast-import: %v\n%s", err, out)
		}
		seen := map[string]bool{}
		prefix := ""
		for _, id := range strings.Fields(r.git(r.dir, "rev-list", "refs/heads/pile")) {
			if seen[id[:7]] {
				prefix = id[:7]
				break
			}
			seen[id[:7]] = true
		}
		if prefix == "" {
			t.Fatal("no 7-digit collision among the generated commits")
		}
		r.wantRefused(r.dir, "ambiguous SHA", "SHA="+prefix)
	})
}

func TestLandShortSHAClass(t *testing.T) {
	// The class comes from the changed paths; a note claiming otherwise changes nothing.
	for _, tc := range []struct {
		name, path, note string
		input, want      string
		lands            bool
	}{
		{"ordinary doc, y", "README.md", "carve-out security stamp", "y\n", "path class: ordinary", true},
		{"exempt package, y", "internal/version/x.go", "ordinary", "y\n", "path class: ordinary", true},
		{"go file claimed ordinary, y refused", "internal/web/x.go", "ordinary change, not a carve-out", "y\n", "answer does not match", false},
		{"Makefile, y refused", "Makefile", "ordinary", "y\n", "answer does not match", false},
		{"scripts, y refused", "scripts/other.sh", "ordinary", "y\n", "answer does not match", false},
		{"AGENTS.md, y refused", "AGENTS.md", "ordinary", "y\n", "answer does not match", false},
		{".agents, y refused", ".agents/helper.md", "ordinary", "y\n", "answer does not match", false},
		{"design page, y refused", "docs/content/design/x.md", "ordinary", "y\n", "answer does not match", false},
		{"rename out of a security path, y refused", "RENAME:internal/version/Makefile", "ordinary", "y\n", "answer does not match", false},
		{"claude.md, y refused", "claude.md", "ordinary", "y\n", "answer does not match", false},
		{"nested claude.md, y refused", "docs/claude.md", "ordinary", "y\n", "answer does not match", false},
		{"Claude.md, y refused", "Claude.md", "ordinary", "y\n", "answer does not match", false},
		{"nested .claude, y refused", "x/.claude/settings.json", "ordinary", "y\n", "answer does not match", false},
		{"nested .agents, y refused", "x/.agents/helper.md", "ordinary", "y\n", "answer does not match", false},
		{".github, y refused", ".github/workflows/x.yml", "ordinary", "y\n", "answer does not match", false},
		{"agents.md lowercase, y refused", "agents.md", "ordinary", "y\n", "answer does not match", false},
		{".CLAUDE dir, y refused", ".CLAUDE/x.md", "ordinary", "y\n", "answer does not match", false},
		{".Agents dir, y refused", ".Agents/x.md", "ordinary", "y\n", "answer does not match", false},
		{"docs .claude agents, y refused", "docs/.claude/agents/y.md", "ordinary", "y\n", "answer does not match", false},
		{"nested AGENTS.md, y refused", "docs/AGENTS.md", "ordinary", "y\n", "answer does not match", false},
		{"top-level other md, y refused", "notes.md", "ordinary", "y\n", "answer does not match", false},
		{"docs .agents, y refused", "docs/.agents/x.md", "ordinary", "y\n", "answer does not match", false},
		{"docs go file, y refused", "docs/x.go", "ordinary", "y\n", "answer does not match", false},
		{"docs markdown, y", "docs/content/guide/x.md", "carve-out", "y\n", "path class: ordinary", true},
		{"carve-out typed back", "internal/web/x.go", "ordinary", "SHORT\n", "path class: carve-out", true},
		{"carve-out typed back, wrong length", "internal/web/x.go", "ordinary", "SHORTX\n", "answer does not match", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			wt := r.topic("topic")
			if err := os.MkdirAll(filepath.Dir(filepath.Join(wt, tc.path)), 0o700); err != nil {
				t.Fatal(err)
			}
			if to, ok := strings.CutPrefix(tc.path, "RENAME:"); ok {
				if err := os.MkdirAll(filepath.Join(wt, filepath.Dir(to)), 0o700); err != nil {
					t.Fatal(err)
				}
				r.git(wt, "mv", "Makefile", to)
			} else {
				r.write(filepath.Join(wt, tc.path), "changed\n")
			}
			r.git(wt, "add", ".")
			r.git(wt, "commit", "-qm", "change")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.git(r.dir, "worktree", "remove", "--force", wt)
			r.stamp(sha, tc.note, sha)
			input := strings.ReplaceAll(tc.input, "SHORT", sha[:7])
			if tc.name == "carve-out typed back, wrong length" {
				input = sha[:7] + "x\n"
			}
			out, err := r.landTTY(r.dir, input, t.TempDir(), nil, "SHA="+sha[:10])
			if !strings.Contains(out, tc.want) {
				t.Fatalf("want %q:\n%s", tc.want, out)
			}
			got := r.git(r.dir, "rev-parse", "main")
			if tc.lands != (err == nil) || tc.lands != (got == sha) {
				t.Fatalf("lands=%v, err=%v, main=%s\n%s", tc.lands, err, got, out)
			}
		})
	}
}

// A candidate that rewrites scripts/land.sh cannot change the decision: the logic
// comes from main's blob, and the change itself makes the class a carve-out.
func TestLandShortSHAMainBlobDecides(t *testing.T) {
	r := newLandBranchRepo(t, false)
	wt := r.topic("topic")
	r.write(filepath.Join(wt, "scripts", "land.sh"), "#!/bin/sh\nprintf '%s %s\\n' \"$(git rev-parse HEAD)\" topic\n")
	r.git(wt, "commit", "-qam", "evil land.sh")
	sha := r.git(wt, "rev-parse", "HEAD")
	r.stamp(sha, "ordinary", sha)
	// checked out in a worktree and not: both must go through main's script
	r.wantTTYRefused(r.dir, "y\n", "answer does not match", nil, "SHA="+sha[:9])
	r.git(r.dir, "worktree", "remove", "--force", wt)
	r.wantTTYRefused(r.dir, "y\n", "answer does not match", nil, "SHA="+sha[:9])
	// the same through the shared checkout's own copy: run from the worktree of the candidate
	wt2 := filepath.Join(t.TempDir(), "wt2")
	r.git(r.dir, "worktree", "add", "-q", wt2, "topic")
	r.wantTTYRefused(wt2, "y\n", "answer does not match", nil, "SHA="+sha[:9])
}

func TestLandShortSHAKeepsGuards(t *testing.T) {
	t.Run("caller-set MAKE variables", func(t *testing.T) {
		for _, tc := range []struct {
			env, args []string
		}{
			{nil, []string{"MAKE=true"}},
			{nil, []string{"MAKE_COMMAND=true"}},
			{[]string{"MAKE=true"}, []string{"-e"}},
			{[]string{"MAKE_COMMAND=true"}, []string{"-e"}},
			{[]string{"MAKE_COMMAND=true"}, nil},
			{[]string{"MAKEFILES=/dev/null"}, nil},
			{[]string{"MAKEFLAGS=MAKE_COMMAND=true"}, nil},
			{[]string{"MAKEFLAGS=MAKE=true"}, nil},
		} {
			r := newLandBranchRepo(t, false)
			sha := r.detachedTopic()
			r.stamp(sha, "ok", sha)
			r.wantTTYRefused(r.dir, "y\n", "MAKE or MAKEFILES is set by the caller", tc.env, append(tc.args, "SHA="+sha[:9])...)
			// the long form refuses before it reads anything else
			wt := r.topic("long")
			full := r.git(wt, "rev-parse", "HEAD")
			base := r.git(r.dir, "rev-parse", "main")
			cmd := exec.CommandContext(t.Context(), "make", append([]string{"-s", "land", "BRANCH=long", "SHA=" + full}, tc.args...)...) //nolint:gosec // fixed make target, isolated repository
			cmd.Dir = r.dir
			cmd.Env = append(r.env(), tc.env...)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "MAKE or MAKEFILES is set by the caller") {
				t.Fatalf("long form %v %v: want refusal, got %v\n%s", tc.env, tc.args, err, out)
			}
			if got := r.git(r.dir, "rev-parse", "main"); got != base {
				t.Fatalf("main moved under %v %v", tc.env, tc.args)
			}
		}
	})
	t.Run("dry-run flags", func(t *testing.T) {
		for _, args := range [][]string{{"-n"}, {"-q"}, {"-t"}, {"--dry-run"}} {
			r := newLandBranchRepo(t, false)
			sha := r.detachedTopic()
			r.stamp(sha, "ok", sha)
			base := r.git(r.dir, "rev-parse", "main")
			tmp := t.TempDir()
			cmd := r.ttyCmd(r.dir, tmp, nil, append(args, "SHA="+sha[:9])...)
			out, _ := r.runTTY(cmd, "y\n")
			if got := r.git(r.dir, "rev-parse", "main"); got != base {
				t.Fatalf("main moved under %v\n%s", args, out)
			}
		}
	})
	t.Run("caller flags do not reach the checks", func(t *testing.T) {
		for _, tc := range []struct {
			env, args []string
		}{
			{nil, []string{"-i"}},
			{[]string{"MAKEFLAGS=i"}, nil},
			{nil, []string{"LAND_CLEAN=true"}},
			{nil, []string{"LAND_MAKE=true"}},
			{[]string{"LAND_CLEAN=true"}, []string{"-e"}},
			{[]string{"LAND_MAKE=true"}, []string{"-e"}},
		} {
			r := newLandBranchRepo(t, false)
			r.stubChecks("check-local:\n\t@echo check-local failed >&2; exit 1\ncommitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n")
			sha := r.detachedTopic()
			r.stamp(sha, "ok", sha)
			base := r.git(r.dir, "rev-parse", "main")
			tmp := t.TempDir()
			cmd := r.ttyCmd(r.dir, tmp, tc.env, append(tc.args, "SHA="+sha[:9])...)
			out, _ := r.runTTY(cmd, "y\n")
			if got := r.git(r.dir, "rev-parse", "main"); got != base {
				t.Fatalf("main moved under %v %v\n%s", tc.env, tc.args, out)
			}
			if !strings.Contains(out, "check-local failed") {
				t.Fatalf("check-local did not run for real under %v %v\n%s", tc.env, tc.args, out)
			}
			r.wantNoTemp(tmp)
		}
	})
	t.Run("check failure cleans the temporary worktree", func(t *testing.T) {
		for _, failing := range []string{"check-local", "commitlint", "test-commitlint-consumers", "secrets-range"} {
			r := newLandBranchRepo(t, false)
			r.stubChecks("check-local commitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n" + failing + ":\n\t@echo " + failing + " failed >&2; exit 1\n")
			sha := r.detachedTopic()
			r.stamp(sha, "ok", sha)
			r.wantTTYRefused(r.dir, "y\n", failing+" failed", nil, "SHA="+sha[:9])
		}
	})
	t.Run("failed cleanup is reported", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root removes anything")
		}
		r := newLandBranchRepo(t, false)
		r.stubChecks("check-local:\n\t@mkdir locked && touch locked/f && chmod 555 locked\ncommitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n")
		sha := r.detachedTopic()
		r.stamp(sha, "ok", sha)
		tmp := t.TempDir()
		t.Cleanup(func() {
			m, _ := filepath.Glob(filepath.Join(tmp, "land.*", "wt", "locked"))
			for _, d := range m {
				_ = os.Chmod(d, 0o700) //nolint:gosec // let the test directory be removed
			}
		})
		out, err := r.landTTY(r.dir, "y\n", tmp, nil, "SHA="+sha[:9])
		if err == nil || !strings.Contains(out, "could not remove the temporary worktree") {
			t.Fatalf("want a cleanup failure, got %v\n%s", err, out)
		}
	})
	t.Run("temporary worktree creation failure", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.stamp(sha, "ok", sha)
		blocked := filepath.Join(t.TempDir(), "nodir")
		r.write(blocked, "file")
		out, err := r.landTTY(r.dir, "y\n", blocked, nil, "SHA="+sha[:9])
		if err == nil || !strings.Contains(out, "cannot create a temporary directory") {
			t.Fatalf("want refusal, got %v\n%s", err, out)
		}
	})
	t.Run("interrupt cleans the temporary worktree", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.stubChecks("check-local:\n\t@touch started; sleep 60\ncommitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n")
		sha := r.detachedTopic()
		r.stamp(sha, "ok", sha)
		base := r.git(r.dir, "rev-parse", "main")
		tmp := t.TempDir()
		cmd := r.ttyCmd(r.dir, tmp, nil, "SHA="+sha[:9])
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out := &syncBuf{}
		cmd.Stdout, cmd.Stderr = out, out
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for !strings.Contains(out.String(), "[y/N] ") && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		_, _ = in.Write([]byte("y\n"))
		var marker string
		for time.Now().Before(deadline) {
			if m, _ := filepath.Glob(filepath.Join(tmp, "land.*", "wt", "started")); len(m) == 1 {
				marker = m[0]
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if marker == "" {
			t.Fatalf("the checks never started in the temporary worktree\n%s", out.String())
		}
		_, _ = in.Write([]byte{3}) // ^C on the pseudo terminal: SIGINT to its foreground group
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			t.Fatalf("land did not stop after the interrupt\n%s", out.String())
		}
		time.Sleep(200 * time.Millisecond)
		if got := r.git(r.dir, "rev-parse", "main"); got != base {
			t.Fatalf("interrupted land moved main to %s", got)
		}
		r.wantNoTemp(tmp)
		if list := r.git(r.dir, "worktree", "list", "--porcelain"); strings.Contains(list, "/land.") {
			t.Fatalf("worktree still registered:\n%s", list)
		}
	})
	t.Run("not on top of main, merges, moved main, moved candidate, shared checkout off main", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.stamp(sha, "ok", sha)
		r.git(r.dir, "commit", "--allow-empty", "-qm", "main moves")
		r.wantTTYRefused(r.dir, "y\n", "not on top of main", nil, "SHA="+sha[:9])

		r = newLandBranchRepo(t, false)
		wt := r.topic("topic")
		r.git(wt, "switch", "-qc", "side", "main")
		r.git(wt, "commit", "--allow-empty", "-qm", "side")
		r.git(wt, "switch", "-q", "topic")
		r.git(wt, "merge", "--no-ff", "-qm", "merge", "side")
		msha := r.git(wt, "rev-parse", "HEAD")
		r.git(r.dir, "worktree", "remove", "--force", wt)
		r.git(r.dir, "branch", "-q", "-D", "side")
		r.stamp(msha, "ok", msha)
		r.wantTTYRefused(r.dir, "y\n", "introduces merge commits", nil, "SHA="+msha[:9])

		r = newLandBranchRepo(t, true)
		sha = r.detachedTopic()
		r.stamp(sha, "ok", sha)
		tmp := t.TempDir()
		out, err := r.landTTY(r.dir, "y\n", tmp, nil, "SHA="+sha[:9])
		if err == nil || !strings.Contains(out, "main moved during the checks") {
			t.Fatalf("want refusal, got %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); got == sha {
			t.Fatal("landed although main moved")
		}
		r.wantNoTemp(tmp)

		r = newLandBranchRepo(t, false)
		r.stubChecks("check-local:\n\t@git update-ref refs/heads/topic \"$$(git commit-tree -p topic -m moved topic^{tree})\"\ncommitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n")
		sha = r.detachedTopic()
		r.stamp(sha, "ok", sha)
		r.wantTTYRefused(r.dir, "y\n", "candidate moved during the checks", nil, "SHA="+sha[:9])

		r = newLandBranchRepo(t, false)
		sha = r.detachedTopic()
		r.stamp(sha, "ok", sha)
		off := filepath.Join(t.TempDir(), "off")
		r.git(r.dir, "worktree", "add", "-q", "--detach", off, "main")
		r.git(r.dir, "switch", "-q", "--detach")
		r.wantTTYRefused(off, "y\n", "is not on main: stop and tell the human", nil, "SHA="+sha[:9])
	})
}

// A full 40-hex SHA without BRANCH= goes through resolve like a short one.
func TestLandFullSHAResolves(t *testing.T) {
	r := newLandBranchRepo(t, false)
	sha := r.detachedTopic()
	r.wantRefused(r.dir, "has no review note", "SHA="+sha)
	r.stamp(sha, "ok", sha)
	r.wantRefused(r.dir, "not a terminal", "SHA="+sha)
	if out, err := r.landTTY(r.dir, "y\n", t.TempDir(), nil, "SHA="+sha); err != nil {
		t.Fatalf("land: %v\n%s", err, out)
	}
	if got := r.git(r.dir, "rev-parse", "main"); got != sha {
		t.Fatalf("main = %s, want %s", got, sha)
	}
}
