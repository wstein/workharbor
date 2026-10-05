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
			write(filepath.Join(dir, "Makefile"), []byte("land:\n"+recipe+checks))
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
