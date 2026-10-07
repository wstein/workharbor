package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLandWizardSelection(t *testing.T) {
	for _, security := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "carve-out"}[security], func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			wt := r.topic("topic")
			if security {
				r.write(filepath.Join(wt, "script.sh"), "security relevant\n")
				r.git(wt, "add", "script.sh")
				r.git(wt, "commit", "-qm", "security change")
			}
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "Reviewed by wh/review", sha)
			answer := "y"
			if security {
				answer = sha[:7]
			}
			out, err := r.wizardTTY(r.dir, "1\n"+answer+"\n", t.TempDir(), nil)
			if err != nil {
				t.Fatalf("wizard: %v\n%s", err, out)
			}
			for _, want := range []string{"1) topic", "review stamp: matched", "checks run before landing", "selected candidate summary", "git push origin main"} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing %q\n%s", want, out)
				}
			}
			if r.git(r.dir, "rev-parse", "main") != sha {
				t.Fatal("selected candidate was not landed")
			}
			r.confirmation(sha)
		})
	}
}

func TestLandWizardRefusals(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"cancel", "q\n", "cancelled"},
		{"invalid", "99\n", "invalid selection"},
		{"decline", "1\nn\n", "not landing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			wt := r.topic("topic")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "review", sha)
			r.wantTTYRefused(r.dir, tc.input, tc.want, nil)
			r.wantNoConfirm()
		})
	}
	t.Run("nonterminal", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.wantRefused(r.dir, "use make land-list or make land-preview")
		r.wantNoConfirm()
	})
	t.Run("no stamps", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.topic("topic")
		r.wantTTYRefused(r.dir, "", "obtain an independent review", nil)
	})
	t.Run("wrong checkout", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.git(r.dir, "switch", "-qc", "other")
		r.wantTTYRefused(r.dir, "", "ask the human to restore main", nil)
		if r.git(r.dir, "symbolic-ref", "--short", "HEAD") != "other" {
			t.Fatal("wizard switched the live checkout")
		}
	})
	t.Run("behind main", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		sha := r.git(wt, "rev-parse", "HEAD")
		r.stamp(sha, "review", sha)
		r.git(r.dir, "commit", "--allow-empty", "-qm", "advance main")
		r.wantTTYRefused(r.dir, "1\nn\n", "obtain review of the new SHA", nil)
		r.wantNoConfirm()
	})
	t.Run("worktree declines", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		r.wantTTYRefused(wt, "n\n", "no checkout was switched", nil)
		if r.git(wt, "symbolic-ref", "--short", "HEAD") != "topic" {
			t.Fatal("wizard switched topic")
		}
	})
	t.Run("worktree agrees", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		sha := r.git(wt, "rev-parse", "HEAD")
		r.stamp(sha, "review", sha)
		out, err := r.wizardTTY(wt, "y\n1\ny\n", t.TempDir(), nil)
		if err != nil {
			t.Fatalf("wizard: %v\n%s", err, out)
		}
		r.confirmation(sha)
	})
}

// Change state after displaying the menu: stale selections must never confirm.
func TestLandWizardStaleSelection(t *testing.T) {
	r := newLandBranchRepo(t, false)
	wt := r.topic("topic")
	sha := r.git(wt, "rev-parse", "HEAD")
	r.stamp(sha, "review", sha)
	base := r.git(r.dir, "rev-parse", "main")
	cmd := r.ttyCmd(r.dir, t.TempDir(), nil)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	defer in.Close()
	for !strings.Contains(out.String(), "q cancels") {
		select {
		case err := <-done:
			t.Fatalf("exited before menu: %v\n%s", err, out.String())
		case <-timer.C:
			_ = cmd.Process.Kill()
			t.Fatal("wizard did not display menu")
		case <-time.After(20 * time.Millisecond):
		}
	}
	r.git(wt, "commit", "--allow-empty", "-qm", "moved after menu")
	if _, err := in.Write([]byte("1\n")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil || !strings.Contains(out.String(), "selected SHA is stale") {
		t.Fatalf("stale selection: %v\n%s", err, out.String())
	}
	if r.git(r.dir, "rev-parse", "main") != base {
		t.Fatal("stale selection moved main")
	}
	r.wantNoConfirm()
}

func TestLandWizardMakeModes(t *testing.T) {
	for _, flag := range []string{"-n", "-t", "-q"} {
		t.Run(flag, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			wt := r.topic("topic")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "review", sha)
			base := r.git(r.dir, "rev-parse", "main")
			out, _ := r.land(r.dir, nil, flag)
			if strings.Contains(out, "land: 1)") || r.git(r.dir, "rev-parse", "main") != base {
				t.Fatalf("mode executed wizard\n%s", out)
			}
			if _, err := os.Stat(filepath.Join(wt, "checks-ran")); !os.IsNotExist(err) {
				t.Fatal("mode ran checks")
			}
			r.wantNoConfirm()
		})
	}
}

// Feed each answer only after its prompt: a nested make can change terminal modes.
func (r *landBranchRepo) wizardTTY(at, input, tmp string, extraEnv []string) (string, error) {
	r.t.Helper()
	cmd := r.ttyCmd(at, tmp, extraEnv)
	in, err := cmd.StdinPipe()
	if err != nil {
		r.t.Fatal(err)
	}
	defer in.Close()
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	answers := strings.Split(strings.TrimSuffix(input, "\n"), "\n")
	sent := 0
	for {
		select {
		case err := <-done:
			return out.String(), err
		case <-timer.C:
			_ = cmd.Process.Kill()
			r.t.Fatalf("wizard stalled\n%s", out.String())
		case <-time.After(20 * time.Millisecond):
			text := out.String()
			prompts := strings.Count(text, "use the shared checkout for this run? [y/N]") + strings.Count(text, "choose a candidate number (q cancels") + strings.Count(text, "onto main? [y/N]") + strings.Count(text, "to land it: ")
			if sent < len(answers) && prompts > sent {
				time.Sleep(100 * time.Millisecond)
				if _, err := in.Write([]byte(answers[sent] + "\n")); err != nil {
					r.t.Fatal(err)
				}
				sent++
			}
		}
	}
}

func TestLandWizardTitleControls(t *testing.T) {
	r := newLandBranchRepo(t, false)
	wt := r.topic("topic")
	// OSC/CSI, C0/DEL, C1, every bidi class, and Unicode line separators.
	title := "日本語\tOSC\x1b]52;c;payload\x07CSI\x1b[2JDEL\x7fC1\u009bBidi\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069Sep\u2028\u2029"
	r.git(wt, "commit", "--allow-empty", "-qm", title)
	sha := r.git(wt, "rev-parse", "HEAD")
	r.stamp(sha, "review", sha)
	base := r.git(r.dir, "rev-parse", "main")
	out, err := r.wizardTTY(r.dir, "q\n", t.TempDir(), nil)
	if err == nil {
		t.Fatal("cancelled title preview succeeded")
	}
	if strings.ContainsAny(out, "\x1b\x07\x7f\u009b\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069\u2028\u2029") {
		t.Fatalf("raw title controls reached terminal: %q", out)
	}
	if !strings.Contains(out, "日本語\tOSC?]52;c;payload?CSI?[2JDEL?C1?Bidi????????????Sep??") {
		t.Fatalf("title was not rendered inert while retaining printable UTF-8/tab: %q", out)
	}
	if r.git(r.dir, "rev-parse", "main") != base {
		t.Fatal("cancelled preview moved main")
	}
	r.wantNoConfirm()
}

func TestLandWizardBranchDisplay(t *testing.T) {
	for _, action := range []string{"cancel", "select", "behind", "picker"} {
		t.Run(action, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			branch := "topic\u202eevil"
			wt := r.topic(branch)
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "review", sha)
			if action == "behind" {
				r.git(r.dir, "commit", "--allow-empty", "-qm", "advance main")
			}
			base := r.git(r.dir, "rev-parse", "main")
			input := "q\n"
			var extra []string
			capture := ""
			if action == "select" {
				input = "1\ny\n"
			}
			if action == "behind" {
				input = "1\nn\n"
			}
			if action == "picker" {
				bin := t.TempDir()
				capture = filepath.Join(t.TempDir(), "picker-input")
				stub := filepath.Join(bin, "fzf")
				r.write(stub, "#!/bin/sh\ncat > \"$FZF_DISPLAY_CAPTURE\"\nhead -n 1 \"$FZF_DISPLAY_CAPTURE\"\n")
				if err := os.Chmod(stub, 0o700); err != nil { //nolint:gosec // isolated executable picker fixture
					t.Fatal(err)
				}
				extra = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "FZF_DISPLAY_CAPTURE=" + capture}
				input = "f\ny\n"
			}
			out, err := r.wizardTTY(r.dir, input, t.TempDir(), extra)
			if !strings.Contains(out, "land: 1) topic?evil — linear topic?evil") {
				t.Fatalf("unsafe numbered display: %q", out)
			}
			if action == "behind" && !strings.Contains(out, "land: topic?evil is not on top of main") {
				t.Fatalf("unsafe refusal display: %q", out)
			}
			if action == "select" || action == "picker" {
				if err != nil || r.git(r.dir, "rev-parse", "main") != sha {
					t.Fatalf("raw Git selector was lost: %v %q", err, out)
				}
				if r.confirmation(sha).Subject.Branch != branch {
					t.Fatal("confirmation used sanitized branch instead of actual ref")
				}
			} else {
				if err == nil || r.git(r.dir, "rev-parse", "main") != base {
					t.Fatalf("refusal moved main: %v %q", err, out)
				}
				r.wantNoConfirm()
			}
			if capture != "" {
				data, err := os.ReadFile(capture) //nolint:gosec // fixed capture file in private test directory
				if err != nil {
					t.Fatal(err)
				}
				if strings.ContainsRune(string(data), '\u202e') || !strings.Contains(string(data), "1 "+sha+" topic?evil") {
					t.Fatalf("unsafe picker display: %q", data)
				}
			}
		})
	}
}

// A review note or branch name must not reach the terminal as a screen clear or
// a bidi override: the inspect/preview output is display text only.
func TestLandInspectOutputIsInert(t *testing.T) {
	const bad = "\x1b[2J\x1b]52;c;x\x07\u202e\u2066"
	check := func(t *testing.T, out string) {
		t.Helper()
		if strings.ContainsAny(out, "\x1b\x07\u202e\u2066") {
			t.Fatalf("raw controls reached the terminal: %q", out)
		}
		if !strings.Contains(out, "topic?evil") || !strings.Contains(out, "?[2J?]52;c;x??") {
			t.Fatalf("display copies missing: %q", out)
		}
	}
	t.Run("wizard", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic\u202eevil")
		sha := r.git(wt, "rev-parse", "HEAD")
		r.stamp(sha, "note "+bad, sha)
		out, _ := r.wizardTTY(r.dir, "q\n", t.TempDir(), nil)
		check(t, out)
	})
	for _, target := range []string{"land-list", "land-preview"} {
		t.Run(target, func(t *testing.T) {
			r := newLandQueueRepo(t)
			wt := r.topic("topic\u202eevil")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "note "+bad, sha)
			args := []string{}
			if target == "land-preview" {
				args = append(args, "SHA="+sha)
			}
			out, err := r.queue(target, args...)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			check(t, out)
		})
	}
}

// The confirmation note is written after the fast-forward, so a pre-existing note
// or a locked notes ref must refuse before main moves.
func TestLandRefusesBeforeUnrecordableConfirmation(t *testing.T) {
	for _, mode := range []string{"existing-note", "locked-ref"} {
		t.Run(mode, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			wt := r.topic("topic")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "review", sha)
			if mode == "existing-note" {
				r.git(r.dir, "notes", "--ref=confirm", "add", "-m", "planted", sha)
			} else {
				gitDir := r.git(r.dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
				lock := filepath.Join(gitDir, "refs", "notes", "confirm.lock")
				r.write(lock, "")
			}
			base := r.git(r.dir, "rev-parse", "main")
			out, err := r.wizardTTY(r.dir, "1\ny\n", t.TempDir(), nil)
			if err == nil || r.git(r.dir, "rev-parse", "main") != base {
				t.Fatalf("main moved or land succeeded: %v\n%s", err, out)
			}
			if !strings.Contains(out, "refusing before main moves") {
				t.Fatalf("no early refusal: %s", out)
			}
		})
	}
}

func TestLandPreviewInertFileNamesAndBranchList(t *testing.T) {
	t.Run("file name", func(t *testing.T) {
		r := newLandQueueRepo(t)
		wt := r.topic("topic")
		r.write(filepath.Join(wt, "x\u202e\u0085y.md"), "x\n")
		r.git(wt, "add", "-A")
		r.git(wt, "commit", "-qm", "named file")
		sha := r.git(wt, "rev-parse", "HEAD")
		r.stamp(sha, "review", sha)
		out, err := r.queue("land-preview", "SHA="+sha)
		if err != nil || strings.ContainsAny(out, "\u202e\u0085") || !strings.Contains(out, "y.md") {
			t.Fatalf("file name reached the terminal raw or preview failed: %v %q", err, out)
		}
	})
	t.Run("several branches", func(t *testing.T) {
		r := newLandQueueRepo(t)
		wt := r.topic("topic")
		sha := r.git(wt, "rev-parse", "HEAD")
		r.stamp(sha, "review", sha)
		r.git(r.dir, "branch", "alias\u202eevil", sha)
		out, err := r.queue("land-preview", "SHA="+sha)
		if err == nil || strings.ContainsAny(out, "\u202e") || !strings.Contains(out, "alias?evil") {
			t.Fatalf("branch list not inert: %v %q", err, out)
		}
	})
}

func TestLandRecordIdempotentOnlyForIdenticalNote(t *testing.T) {
	r := newLandBranchRepo(t, false)
	wt := r.topic("topic")
	sha := r.git(wt, "rev-parse", "HEAD")
	r.stamp(sha, "review", sha)
	base := r.git(r.dir, "rev-parse", "main")
	review := r.git(r.dir, "notes", "--ref=review", "list", sha)
	r.git(r.dir, "merge", "-q", "--ff-only", sha)
	record := func(at string) error {
		cmd := exec.CommandContext(t.Context(), "sh", filepath.Join(r.dir, "scripts", "land.sh"), "record", sha, "topic", "yn", "yes", at, review, base, "ordinary", "0") //nolint:gosec // fixture script in an isolated repository
		cmd.Dir, cmd.Env = r.dir, r.env()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("%s", out)
		}
		return err
	}
	if err := record("2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	first := r.git(r.dir, "notes", "--ref=confirm", "list", sha)
	if err := record("2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("identical note refused: %v", err)
	}
	if err := record("2026-01-02T00:00:00Z"); err == nil {
		t.Fatal("different note accepted")
	}
	if r.git(r.dir, "notes", "--ref=confirm", "list", sha) != first {
		t.Fatal("existing note was replaced")
	}
}
