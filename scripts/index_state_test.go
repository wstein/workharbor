package scripts_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=/nonexistent",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
	}
}

func TestIndexState(t *testing.T) {
	cases := []struct {
		name  string
		setup []string
		want  int
	}{
		{"clean", nil, 0},
		{"modified in index, tree equals HEAD", []string{"echo x>f", "git add f", "echo a>f"}, 3},
		{"missing from index, file present", []string{"git rm -q --cached g"}, 3},
		{"real staged work", []string{"echo x>f", "git add f"}, 4},
		{"real staged new file", []string{"echo n>h", "git add h"}, 4},
		{"stale path and real staged path", []string{"git rm -q --cached g", "echo x>f", "git add f"}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			run := func(cmd string) {
				c := exec.CommandContext(t.Context(), "sh", "-c", cmd) //nolint:gosec // a test command
				c.Dir = dir
				c.Env = gitEnv()
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", cmd, err, out)
				}
			}
			run("git init -q -b main && echo a>f && echo b>g && git add . && git commit -q -m init")
			for _, s := range tc.setup {
				run(s)
			}
			before, _ := os.ReadFile(filepath.Join(dir, ".git", "index"))      //nolint:gosec // a test path
			c := exec.CommandContext(t.Context(), "sh", "index-state.sh", dir) //nolint:gosec // a test script
			c.Env = gitEnv()
			err := c.Run()
			got := 0
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				got = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
			after, _ := os.ReadFile(filepath.Join(dir, ".git", "index")) //nolint:gosec // a test path
			if string(before) != string(after) {
				t.Error("the guard wrote the index")
			}
		})
	}
}
