package scripts_test

import (
	"path/filepath"
	"testing"
)

func (r *landBranchRepo) orderMore(wt, path, content string) string {
	r.t.Helper()
	r.write(filepath.Join(wt, path), content)
	r.git(wt, "add", path)
	r.git(wt, "commit", "-qm", "change "+path)
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
