// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("fixture git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func TestLocalStageGitCapturePreservesHistoryAndDirtyIndex(t *testing.T) {
	src := t.TempDir()
	fixtureGit(t, src, "init", "--template=", "--initial-branch=main")
	file := filepath.Join(src, "file")
	if e := os.WriteFile(file, []byte("first\n"), 0644); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, src, "add", "file")
	fixtureGit(t, src, "commit", "-m", "first")
	first := fixtureGit(t, src, "rev-parse", "HEAD")
	if e := os.WriteFile(file, []byte("second\n"), 0644); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, src, "add", "file")
	fixtureGit(t, src, "commit", "-m", "second")
	head := fixtureGit(t, src, "rev-parse", "HEAD")
	if e := os.WriteFile(file, []byte("staged\n"), 0644); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, src, "add", "file")
	if e := os.WriteFile(file, []byte("working\n"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(src, "untracked"), []byte("retained\n"), 0644); e != nil {
		t.Fatal(e)
	}
	sentinel := filepath.Join(t.TempDir(), "executed")
	fixtureGit(t, src, "config", "core.fsmonitor", "touch "+sentinel)
	fixtureGit(t, src, "config", "core.sshCommand", "touch "+sentinel)
	capture, e := captureStageFiles(context.Background(), src, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer capture.Close()
	clone := filepath.Join(t.TempDir(), "clone")
	got, e := captureStageGit(context.Background(), src, capture.Path, clone)
	if e != nil {
		t.Fatal(e)
	}
	if got.Head != head {
		t.Fatalf("head %s want %s", got.Head, head)
	}
	if fixtureGit(t, clone, "rev-parse", "HEAD") != head || fixtureGit(t, clone, "cat-file", "-t", first) != "commit" {
		t.Fatal("full history was lost")
	}
	if fixtureGit(t, clone, "show", ":file") != "staged" {
		t.Fatal("index was lost")
	}
	b, _ := os.ReadFile(filepath.Join(clone, "file"))
	if string(b) != "working\n" {
		t.Fatal("working bytes were lost")
	}
	if b, _ = os.ReadFile(filepath.Join(clone, "untracked")); string(b) != "retained\n" {
		t.Fatal("untracked bytes were lost")
	}
	if _, e = os.Stat(sentinel); !os.IsNotExist(e) {
		t.Fatalf("source config executed: %v", e)
	}
	if _, e = os.Stat(filepath.Join(clone, ".git", "objects", "info", "alternates")); !os.IsNotExist(e) {
		t.Fatalf("clone retains alternates: %v", e)
	}
}
