package scripts_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// orderCommit adds a worktree on a new branch from main (or checks out the
// existing one) and commits path with content; it returns the worktree and sha.
func (r *landBranchRepo) orderCommit(branch, content string) (wt, sha string) {
	r.t.Helper()
	wt = filepath.Join(r.t.TempDir(), "wt")
	r.git(r.dir, "worktree", "add", "-q", "-b", branch, wt, "main")
	return wt, r.orderMore(wt, "AGENTS.md", content)
}

func (r *landBranchRepo) orderMore(wt, path, content string) string {
	r.t.Helper()
	r.write(filepath.Join(wt, path), content)
	r.git(wt, "add", path)
	r.git(wt, "commit", "-qm", "change "+path)
	return r.git(wt, "rev-parse", "HEAD")
}

// orderPick re-applies sha on top of the worktree's HEAD with a new message: a
// different commit with an equal (or, if edit is set, a changed) patch.
func (r *landBranchRepo) orderPick(wt, sha, edit string) string {
	r.t.Helper()
	r.git(wt, "cherry-pick", "-n", sha)
	if edit != "" {
		r.write(filepath.Join(wt, "AGENTS.md"), edit)
		r.git(wt, "add", "AGENTS.md")
	}
	r.git(wt, "commit", "-qm", "rebased")
	return r.git(wt, "rev-parse", "HEAD")
}

func (r *landBranchRepo) clearBy(sha, model string) {
	r.t.Helper()
	r.git(r.dir, "notes", "--ref=review", "add", "-f", "-m", "CLEAR "+sha+" role=review model="+model, sha)
}

func (r *landBranchRepo) previewOrder(sha string) (string, error) {
	r.t.Helper()
	return r.queue("land-preview", "SHA="+sha)
}

func TestLandPatchIDInheritance(t *testing.T) {
	const opus, sonnet = "claude-opus-4", "claude-sonnet-4"
	for _, tc := range []struct {
		name       string
		origModel  string // "" means the original has no note
		edit       string // changed content of the rebased commit
		tipModel   string
		wantRefuse bool
	}{
		{"equal patch-id, sonnet tip, opus original passes", opus, "", sonnet, false},
		{"equal patch-id, opus tip passes", opus, "", opus, false},
		{"changed patch refuses", opus, "other\n", sonnet, true},
		{"missing original note refuses", "", "", sonnet, true},
		{"sonnet-only original refuses", sonnet, "", sonnet, true},
		{"not-opus model is not opus", "not-opus", "", sonnet, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLandQueueRepo(t)
			_, orig := r.orderCommit("orig", "rule\n")
			if tc.origModel != "" {
				r.clearBy(orig, tc.origModel)
			}
			// A rebased copy of orig on its own branch.
			wt2 := filepath.Join(t.TempDir(), "wt")
			r.git(r.dir, "worktree", "add", "-q", "-b", "copy", wt2, "main")
			tip := r.orderPick(wt2, orig, tc.edit)
			r.clearBy(tip, tc.tipModel)
			out, err := r.previewOrder(tip)
			if tc.wantRefuse != (err != nil) {
				t.Fatalf("refuse=%v, err=%v\n%s", tc.wantRefuse, err, out)
			}
			if tc.wantRefuse && !strings.Contains(out, "no CLEAR line from an Opus model") {
				t.Fatalf("wrong refusal:\n%s", out)
			}
		})
	}
	t.Run("sonnet-only note on new security-relevant content refuses", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, sha := r.orderCommit("topic", "rule\n")
		r.clearBy(sha, sonnet)
		if out, err := r.previewOrder(sha); err == nil || !strings.Contains(out, "no CLEAR line from an Opus model") {
			t.Fatalf("want refusal: %v\n%s", err, out)
		}
	})
}

func TestLandOrder(t *testing.T) {
	const opus, sonnet = "claude-opus-4", "claude-sonnet-4"
	// stack builds originals a, b (Opus CLEAR) and copies a2, b2 on branch landing.
	stack := func(t *testing.T) (r *landBranchRepo, a, a2, b2 string) {
		r = newLandQueueRepo(t)
		wtO, a0 := r.orderCommit("orig", "one\n")
		a = a0
		b := r.orderMore(wtO, "CLAUDE.md", "two\n")
		r.clearBy(a, opus)
		r.clearBy(b, opus)
		wt := filepath.Join(t.TempDir(), "wt")
		r.git(r.dir, "worktree", "add", "-q", "-b", "landing", wt, "main")
		a2 = r.orderPick(wt, a, "")
		b2 = r.orderPick(wt, b, "")
		return
	}
	t.Run("full stack accepted, landing and original at one SHA are one landing", func(t *testing.T) {
		r, _, a2, b2 := stack(t)
		r.clearBy(a2, sonnet)
		r.clearBy(b2, sonnet)
		r.git(r.dir, "branch", "same", b2)
		if out, err := r.previewOrder(b2); err != nil {
			t.Fatalf("want accepted: %v\n%s", err, out)
		}
	})
	t.Run("out of order refused", func(t *testing.T) {
		r, a, _, b2 := stack(t)
		r.clearBy(b2, sonnet)
		r.git(r.dir, "notes", "--ref=review", "remove", a) // the earlier member has no evidence
		if out, err := r.previewOrder(b2); err == nil || !strings.Contains(out, "landing order") {
			t.Fatalf("want landing-order refusal: %v\n%s", err, out)
		}
	})
	t.Run("stack member with a changed patch refused", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, a := r.orderCommit("orig", "one\n")
		r.clearBy(a, opus)
		wt := filepath.Join(t.TempDir(), "wt")
		r.git(r.dir, "worktree", "add", "-q", "-b", "landing", wt, "main")
		a2 := r.orderPick(wt, a, "changed\n")
		r.clearBy(a2, sonnet)
		if out, err := r.previewOrder(a2); err == nil {
			t.Fatalf("want refusal:\n%s", out)
		}
	})
}

func (r *landBranchRepo) rawNote(sha, text string) {
	r.t.Helper()
	r.git(r.dir, "notes", "--ref=review", "add", "-f", "-m", text, sha)
}

func TestLandVerbatimAndNotClear(t *testing.T) {
	const opus, sonnet = "claude-opus-4", "claude-sonnet-4"
	copyOf := func(r *landBranchRepo, orig, edit string) string {
		wt := filepath.Join(r.t.TempDir(), "wt")
		r.git(r.dir, "worktree", "add", "-q", "-b", "copy"+orig[:6], wt, "main")
		return r.orderPick(wt, orig, edit)
	}
	t.Run("whitespace-only change does not inherit", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, orig := r.orderCommit("orig", "rm -rf \"$a b\"\n")
		r.clearBy(orig, opus)
		tip := copyOf(r, orig, "rm -rf \"$ab\"\n")
		r.clearBy(tip, sonnet)
		if out, err := r.previewOrder(tip); err == nil || !strings.Contains(out, "Opus") {
			t.Fatalf("want refusal: %v\n%s", err, out)
		}
	})
	t.Run("identical cherry-pick inherits and names the original", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, orig := r.orderCommit("orig", "rule\n")
		r.clearBy(orig, opus)
		tip := copyOf(r, orig, "")
		r.clearBy(tip, sonnet)
		out, err := r.previewOrder(tip)
		if err != nil || !strings.Contains(out, "covered by "+orig[:7]+" (patch-id)") {
			t.Fatalf("want inherit: %v\n%s", err, out)
		}
	})
	t.Run("equivalent original with NOT CLEAR does not cover", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, o1 := r.orderCommit("o1", "rule\n")
		r.clearBy(o1, opus)
		_, o2 := r.orderCommit("o2", "rule\n")
		r.rawNote(o2, "NOT CLEAR "+o2+" role=review model="+opus)
		tip := copyOf(r, o1, "")
		r.clearBy(tip, sonnet)
		if out, err := r.previewOrder(tip); err == nil {
			t.Fatalf("want refusal:\n%s", out)
		}
	})
	t.Run("own NOT CLEAR on a stack member beats an equivalent CLEAR", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, orig := r.orderCommit("orig", "one\n")
		r.clearBy(orig, opus)
		wt := filepath.Join(t.TempDir(), "wt")
		r.git(r.dir, "worktree", "add", "-q", "-b", "landing", wt, "main")
		a2 := r.orderPick(wt, orig, "")
		b2 := r.orderMore(wt, "AGENTS.md", "two\n")
		r.rawNote(a2, "NOT CLEAR "+a2+" role=review model="+opus)
		r.clearBy(b2, sonnet)
		if out, err := r.previewOrder(b2); err == nil {
			t.Fatalf("want refusal:\n%s", out)
		}
	})
	t.Run("two-commit branch reviewed on its tip passes on landing", func(t *testing.T) {
		r := newLandQueueRepo(t)
		wt, _ := r.orderCommit("topic", "one\n")
		tip := r.orderMore(wt, "CLAUDE.md", "two\n")
		r.clearBy(tip, opus)
		r.git(r.dir, "branch", "landing", tip)
		if out, err := r.previewOrder(tip); err != nil {
			t.Fatalf("want accepted: %v\n%s", err, out)
		}
	})
	t.Run("lookalike opus tip model on new content refuses", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, sha := r.orderCommit("topic", "rule\n")
		r.clearBy(sha, "not-opus")
		if out, err := r.previewOrder(sha); err == nil || !strings.Contains(out, "Opus") {
			t.Fatalf("want refusal: %v\n%s", err, out)
		}
	})
	t.Run("CRLF notes", func(t *testing.T) {
		r := newLandQueueRepo(t)
		_, sha := r.orderCommit("topic", "rule\n")
		line := "CLEAR " + sha + " role=review model=" + opus + "\r\n"
		r.rawNote(sha, line)
		if out, err := r.previewOrder(sha); err != nil {
			t.Fatalf("CRLF CLEAR: %v\n%s", err, out)
		}
		r.rawNote(sha, line+"NOT CLEAR "+sha+"\r\n")
		if out, err := r.previewOrder(sha); err == nil {
			t.Fatalf("CR NOT CLEAR must refuse:\n%s", out)
		}
	})
}

func TestLandLandingPointerSelection(t *testing.T) {
	mk := func(t *testing.T, names ...string) (*landBranchRepo, string) {
		r := newLandQueueRepo(t)
		wt := filepath.Join(t.TempDir(), "wt")
		r.git(r.dir, "worktree", "add", "-q", "-b", names[0], wt, "main")
		sha := r.orderMore(wt, "AGENTS.md", "rule\n")
		r.clearBy(sha, "claude-opus-4")
		for _, n := range names[1:] {
			r.git(r.dir, "branch", n, sha)
		}
		return r, sha
	}
	for _, tc := range []struct {
		name    string
		names   []string
		wantErr bool
	}{
		{"landing and one original land the original", []string{"orig", "landing"}, false},
		{"only landing works", []string{"landing"}, false},
		{"two real branches plus landing refuse", []string{"a", "b", "landing"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sha := mk(t, tc.names...)
			out, err := r.previewOrder(sha)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err=%v\n%s", err, out)
			}
		})
	}
}

func TestLandMalformedNotClearRefuses(t *testing.T) {
	const opus = "claude-opus-4"
	other := strings.Repeat("b", 40)
	for _, tc := range []struct {
		name    string
		line    func(sha string) string
		refuses bool
	}{
		{"lowercase", func(s string) string { return "not clear " + s }, true},
		{"double space", func(s string) string { return "NOT  CLEAR " + s }, true},
		{"tab", func(s string) string { return "NOT\tCLEAR " + s }, true},
		{"leading blank", func(s string) string { return " NOT CLEAR " + s }, true},
		{"colon", func(s string) string { return "NOT CLEAR: " + s }, true},
		{"short sha", func(s string) string { return "NOT CLEAR " + s[:12] }, true},
		{"no sha", func(string) string { return "NOT CLEAR" }, true},
		{"another full sha", func(string) string { return "NOT CLEAR " + other }, false},
		{"word merely starting with not clear", func(string) string { return "NOT CLEARLY fine" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLandQueueRepo(t)
			_, sha := r.orderCommit("topic", "rule\n")
			r.rawNote(sha, "CLEAR "+sha+" role=review model="+opus+"\n"+tc.line(sha))
			out, err := r.previewOrder(sha)
			if tc.refuses != (err != nil) {
				t.Fatalf("refuses=%v, err=%v\n%s", tc.refuses, err, out)
			}
		})
	}
}
