package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// reviewClassRepo makes a repo with base commit "main" and a branch "topic"
// that adds the given files.
func reviewClassRepo(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // fixed git
		cmd.Dir = dir
		cmd.Env = []string{
			"PATH=/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin", "HOME=" + t.TempDir(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "base")
	run("switch", "-q", "-c", "topic")
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "change")
	return dir
}

func TestReviewClass(t *testing.T) {
	t.Parallel()
	script, err := filepath.Abs("review-class.sh")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"ordinary", []string{"docs/guide.md", "README.md"}, []string{"class: ordinary", "required: review/sonnet or review/opus"}},
		{"carve-out", []string{"scripts/x.sh"}, []string{"class: carve-out", "required: review/opus"}},
		{"mixed", []string{"README.md", ".agents/dispatch.md"}, []string{"class: carve-out", "required: review/opus"}},
		{"design mock readme", []string{"design/mock/README.md"}, []string{"class: carve-out", "required: review/opus"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := reviewClassRepo(t, tc.files...)
			cmd := exec.CommandContext(t.Context(), "sh", script, "topic", "main") //nolint:gosec // fixed script
			cmd.Dir = dir
			cmd.Env = []string{"PATH=/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin", "HOME=" + t.TempDir()}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("review-class.sh: %v\n%s", err, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(string(out), w+"\n") {
					t.Errorf("output %q lacks %q", out, w)
				}
			}
		})
	}
}
