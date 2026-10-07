package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/confirm"
)

func (r *landBranchRepo) wantNoConfirm() {
	r.t.Helper()
	if notes := r.git(r.dir, "notes", "--ref=confirm", "list"); notes != "" {
		r.t.Fatalf("unexpected confirmation notes: %s", notes)
	}
}

func (r *landBranchRepo) confirmation(sha string) confirm.Record {
	r.t.Helper()
	object := r.git(r.dir, "notes", "--ref=confirm", "list", sha)
	cmd := exec.CommandContext(r.t.Context(), "git", "cat-file", "blob", object) //nolint:gosec // note object in an isolated repository
	cmd.Dir, cmd.Env = r.dir, r.env()
	blob, err := cmd.Output()
	if err != nil {
		r.t.Fatal(err)
	}
	// Git notes stripspace adds one final LF; it is storage framing, not part
	// of the canonical payload or its digest. Do not trim arbitrary whitespace.
	if !bytes.HasSuffix(blob, []byte("\n")) {
		r.t.Fatalf("note lacks final LF: %q", blob)
	}
	raw := bytes.TrimSuffix(blob, []byte("\n"))
	record, err := confirm.Decode(raw)
	if err != nil {
		r.t.Fatalf("confirmation is not canonical v1: %v\n%s", err, raw)
	}
	again, err := confirm.Encode(record)
	if err != nil || !bytes.Equal(raw, again) {
		r.t.Fatalf("Go/Python canonical encoding differs: %v\n%s\n%s", err, raw, again)
	}
	return record
}

func TestLandConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, branch, path, input string
		mode, class               string
		checks                    []string
	}{
		{"ordinary", "topic", "README.md", "y\n", confirm.ModeYN, "ordinary", nil},
		{"uppercase answer and escaped Unicode branch", "topic/caf\u00e9-\U0001f600-\"quote", "README.md", "Y\n", confirm.ModeYN, "ordinary", nil},
		{"carve-out", "topic", "AGENTS.md", "SHORT\n", confirm.ModeTypedSHA, "carve-out", nil},
		{"generated check", "topic", "internal/web/fixture.templ", "SHORT\n", confirm.ModeTypedSHA, "carve-out", []string{"check-generated"}},
		{"candidate writer cannot replace main writer", "topic", "scripts/land.sh", "SHORT\n", confirm.ModeTypedSHA, "carve-out", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			base := r.git(r.dir, "rev-parse", "main")
			wt := r.topic(tc.branch)
			if err := os.MkdirAll(filepath.Join(wt, filepath.Dir(tc.path)), 0o700); err != nil {
				t.Fatal(err)
			}
			r.write(filepath.Join(wt, tc.path), "candidate writer must never run\nexit 1\n")
			r.git(wt, "add", tc.path)
			r.git(wt, "commit", "-qm", "candidate")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "Reviewed by wh/review", sha)
			review := r.git(r.dir, "notes", "--ref=review", "list", sha)
			start := time.Now().UTC().Truncate(time.Second)
			input := strings.ReplaceAll(tc.input, "SHORT", sha[:7])
			out, err := r.landTTY(r.dir, input, t.TempDir(), nil, "SHA="+sha[:9])
			if err != nil {
				t.Fatalf("land: %v\n%s", err, out)
			}
			if r.git(r.dir, "rev-parse", "main") != sha {
				t.Fatal("confirmation was not preceded by landing")
			}
			record := r.confirmation(sha)
			answer := "yes"
			if tc.mode == confirm.ModeTypedSHA {
				answer = sha[:7]
			}
			if record.Action != confirm.ActionLand || record.Subject != (confirm.Subject{Commit: sha, Branch: tc.branch}) ||
				record.Answer != (confirm.Answer{Mode: tc.mode, Value: answer}) || record.Channel != confirm.ChannelCLI ||
				record.Assurance != confirm.AssuranceLocal || record.By != "human" {
				t.Fatalf("wrong confirmation: %+v", record)
			}
			at, err := time.Parse(time.RFC3339, record.At)
			if err != nil || at.Before(start) || at.After(time.Now().UTC()) {
				t.Fatalf("confirmation time %q: %v", record.At, err)
			}
			checks := map[string]string{}
			for _, e := range record.Evidence {
				switch e.Kind {
				case confirm.EvidenceCheck:
					checks[e.Name] = e.Result
				case confirm.EvidenceBase:
					if e.Object != base {
						t.Fatalf("base = %s, want %s", e.Object, base)
					}
				case confirm.EvidencePathClass:
					if e.Value != tc.class {
						t.Fatalf("class = %s, want %s", e.Value, tc.class)
					}
				case confirm.EvidenceReviewNote:
					if e.Object != review || e.Ref != "refs/notes/review" {
						t.Fatalf("review evidence: %+v", e)
					}
				default:
					t.Fatalf("unexpected evidence: %+v", e)
				}
			}
			want := map[string]string{"check-local": "pass", "commitlint": "pass", "test-commitlint-consumers": "pass", "secrets-range": "pass"}
			for _, name := range tc.checks {
				want[name] = "pass"
			}
			if !reflect.DeepEqual(checks, want) || len(record.Evidence) != len(want)+3 {
				t.Fatalf("check evidence %v, want %v; evidence %+v", checks, want, record.Evidence)
			}
		})
	}
}

func TestLandConfirmationRetainsDisplayedReview(t *testing.T) {
	r := newLandBranchRepo(t, false)
	r.stubChecks("check-local:\n\t@git notes --ref=review add -f -m \"changed at $$(git rev-parse HEAD)\" HEAD\ncommitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n")
	sha := r.detachedTopic()
	r.stamp(sha, "displayed review", sha)
	displayed := r.git(r.dir, "notes", "--ref=review", "list", sha)
	out, err := r.landTTY(r.dir, "y\n", t.TempDir(), nil, "SHA="+sha)
	if err != nil {
		t.Fatalf("land: %v\n%s", err, out)
	}
	if r.git(r.dir, "notes", "--ref=review", "list", sha) == displayed {
		t.Fatal("review mutation did not run")
	}
	for _, e := range r.confirmation(sha).Evidence {
		if e.Kind == confirm.EvidenceReviewNote {
			if e.Object != displayed {
				t.Fatalf("record cites undisplayed review: %+v", e)
			}
			return
		}
	}
	t.Fatal("review evidence missing")
}

func TestLandConfirmationRefusals(t *testing.T) {
	for _, failing := range []string{"check-local", "commitlint", "test-commitlint-consumers", "secrets-range", "check-generated"} {
		t.Run(failing, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			r.stubChecks("check-local commitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n" + failing + ":\n\t@echo failed-required-check >&2; exit 1\n")
			wt := r.topic("topic")
			path := filepath.Join(wt, "internal", "web", "fixture.templ")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			r.write(path, "template\n")
			r.git(wt, "add", ".")
			r.git(wt, "commit", "-qm", "template")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "review", sha)
			r.wantTTYRefused(r.dir, sha[:7]+"\n", "failed-required-check", nil, "SHA="+sha)
		})
	}
	for _, tc := range []struct{ name, mutation, message string }{
		{"candidate moves", "git commit --allow-empty -qm moved", "candidate moved during the checks"},
		{"main moves", "git update-ref refs/heads/main \"$$(git commit-tree -p main -m moved main^{tree})\"", "main moved during the checks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			r.stubChecks("check-local:\n\t@" + tc.mutation + "\ncommitlint test-commitlint-consumers check-generated secrets-range:\n\t@:\n")
			wt := r.topic("topic")
			sha := r.git(wt, "rev-parse", "HEAD")
			r.stamp(sha, "review", sha)
			out, err := r.landTTY(r.dir, "y\n", t.TempDir(), nil, "SHA="+sha)
			if err == nil || !strings.Contains(out, tc.message) {
				t.Fatalf("mutation: %v\n%s", err, out)
			}
			r.wantNoConfirm()
		})
	}
	t.Run("fast-forward fails", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		sha := r.detachedTopic()
		r.stamp(sha, "review", sha)
		// A tracked, modified file makes the actual git merge refuse.
		wt := filepath.Join(t.TempDir(), "topic")
		r.git(r.dir, "worktree", "add", "-q", wt, "topic")
		r.write(filepath.Join(wt, "tracked.txt"), "candidate\n")
		r.git(wt, "commit", "-qam", "tracked change")
		sha = r.git(wt, "rev-parse", "HEAD")
		r.stamp(sha, "review", sha)
		r.write(filepath.Join(r.dir, "tracked.txt"), "human local edit\n")
		r.wantTTYRefused(r.dir, sha[:7]+"\n", "would be overwritten by merge", nil, "SHA="+sha)
	})
}

func TestLandConfirmationWriterFailure(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "encoder fails after ff", true: "existing note is preserved"}[existing], func(t *testing.T) {
			r := newLandBranchRepo(t, false)
			sha := r.detachedTopic()
			r.stamp(sha, "review", sha)
			var env []string
			if existing {
				r.git(r.dir, "notes", "--ref=confirm", "add", "-m", "existing record", sha)
			} else {
				bin := t.TempDir()
				r.write(filepath.Join(bin, "python3"), "#!/bin/sh\nexit 1\n")
				if err := os.Chmod(filepath.Join(bin, "python3"), 0o700); err != nil { //nolint:gosec // test executable
					t.Fatal(err)
				}
				// Keep the isolated test environment, changing only its executable path.
				for _, entry := range r.env() {
					if strings.HasPrefix(entry, "PATH=") {
						env = []string{"PATH=" + bin + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")}
					}
				}
			}
			out, err := r.landTTY(r.dir, "y\n", t.TempDir(), env, "SHA="+sha)
			if err == nil || !strings.Contains(out, "main moved to "+sha+", but its confirmation note was not recorded") {
				t.Fatalf("writer failure: %v\n%s", err, out)
			}
			if r.git(r.dir, "rev-parse", "main") != sha {
				t.Fatal("writer failure undid successful ff")
			}
			if existing {
				if r.git(r.dir, "notes", "--ref=confirm", "show", sha) != "existing record" {
					t.Fatal("writer replaced existing note")
				}
			} else {
				r.wantNoConfirm()
			}
		})
	}
}

func TestLandConfirmationNoInventedAnswer(t *testing.T) {
	for _, withSHA := range []bool{false, true} {
		r := newLandBranchRepo(t, false)
		wt := r.topic("topic")
		args := []string{"BRANCH=topic"}
		if withSHA {
			args = append(args, "SHA="+r.git(wt, "rev-parse", "HEAD"))
		}
		out, err := r.land(wt, nil, args...)
		if err != nil {
			t.Fatalf("unprompted land: %v\n%s", err, out)
		}
		r.wantNoConfirm()
	}
}

func TestLandConfirmationRecordRejectsUnlandedCommit(t *testing.T) {
	r := newLandBranchRepo(t, false)
	sha := r.detachedTopic()
	r.stamp(sha, "review", sha)
	cmd := exec.CommandContext(t.Context(), "sh", "scripts/land.sh", "record", sha, "topic", "yn", "yes", "2026-10-06T09:12:00Z", r.git(r.dir, "notes", "--ref=review", "list", sha), r.git(r.dir, "rev-parse", "main"), "ordinary", "0") //nolint:gosec // fixed script and record fields in an isolated repository
	cmd.Dir, cmd.Env = r.dir, r.env()
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "main is not the confirmed commit") {
		t.Fatalf("unlanded record: %v\n%s", err, out)
	}
	r.wantNoConfirm()
}
