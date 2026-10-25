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
	cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "gc.autoDetach=false"}, args...)...)
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

func TestLocalStageGitFormatsRefsAndSafeConfig(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			src := t.TempDir()
			fixtureGit(t, src, "init", "--template=", "--initial-branch=main", "--object-format="+format)
			if e := os.WriteFile(filepath.Join(src, "file"), []byte("base\n"), 0644); e != nil {
				t.Fatal(e)
			}
			fixtureGit(t, src, "add", "file")
			fixtureGit(t, src, "commit", "-m", "base")
			head := fixtureGit(t, src, "rev-parse", "HEAD")
			fixtureGit(t, src, "tag", "version", head)
			fixtureGit(t, src, "update-ref", "refs/remotes/upstream/main", head)
			fixtureGit(t, src, "remote", "add", "upstream", "https://example.invalid/project")
			fixtureGit(t, src, "config", "branch.main.remote", "upstream")
			fixtureGit(t, src, "config", "branch.main.merge", "refs/heads/main")
			fixtureGit(t, src, "remote", "add", "secret", "https://token:password@example.invalid/project")
			fixtureGit(t, src, "remote", "add", "scpsecret", "user:scpcredential@host:path")
			sentinel := filepath.Join(t.TempDir(), "executed")
			config, e := os.OpenFile(filepath.Join(src, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
			if e != nil {
				t.Fatal(e)
			}
			_, e = config.WriteString("\n[uploadpack]\n\tpackObjectsHook = touch " + sentinel + "\n[core]\n\tfsmonitor = touch " + sentinel + "\n[include]\n\tpath = /nonexistent/untrusted-include\n")
			config.Close()
			if e != nil {
				t.Fatal(e)
			}
			files, e := captureStageFiles(context.Background(), src, t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer files.Close()
			clone := filepath.Join(t.TempDir(), "clone")
			got, e := captureStageGit(context.Background(), src, files.Path, clone)
			if e != nil {
				t.Fatal(e)
			}
			if got.ObjectFormat != format || got.Head != head || got.HeadRef != "refs/heads/main" {
				t.Fatalf("capture identity: %+v", got.stageGitSnapshot)
			}
			for _, ref := range []string{"HEAD", "refs/heads/main", "refs/tags/version", "refs/remotes/upstream/main"} {
				if fixtureGit(t, clone, "rev-parse", ref) != head {
					t.Fatalf("lost admitted ref %s", ref)
				}
			}
			if fixtureGit(t, clone, "symbolic-ref", "HEAD") != "refs/heads/main" {
				t.Fatal("branch identity lost")
			}
			if fixtureGit(t, clone, "config", "remote.upstream.url") != "https://example.invalid/project" {
				t.Fatal("safe remote lost")
			}
			cfg, e := os.ReadFile(filepath.Join(clone, ".git", "config"))
			if e != nil {
				t.Fatal(e)
			}
			for _, forbidden := range []string{"password", "token:", "scpcredential", "packObjectsHook", "fsmonitor", "untrusted-include", "git-capture-"} {
				if strings.Contains(string(cfg), forbidden) {
					t.Fatalf("unsafe initial config retained %q", forbidden)
				}
			}
			if _, e = os.Stat(sentinel); !os.IsNotExist(e) {
				t.Fatal("source executable config ran")
			}
		})
	}
}

func TestLocalStageGitPreservesDirtyPopulatedSubmodule(t *testing.T) {
	src := t.TempDir()
	fixtureGit(t, src, "init", "--template=", "--initial-branch=main")
	module := t.TempDir()
	fixtureGit(t, module, "init", "--template=", "--initial-branch=main")
	if e := os.WriteFile(filepath.Join(module, "file"), []byte("module base\n"), 0644); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, module, "add", "file")
	fixtureGit(t, module, "commit", "-m", "module base")
	fixtureGit(t, src, "-c", "protocol.file.allow=always", "submodule", "add", module, "child")
	fixtureGit(t, src, "commit", "-m", "root")
	child := filepath.Join(src, "child")
	if e := os.WriteFile(filepath.Join(child, "file"), []byte("module staged\n"), 0644); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, child, "add", "file")
	if e := os.WriteFile(filepath.Join(child, "file"), []byte("module working\n"), 0644); e != nil {
		t.Fatal(e)
	}
	before := fixtureGit(t, child, "status", "--porcelain=v1")
	files, e := captureStageFiles(context.Background(), src, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer files.Close()
	clone := filepath.Join(t.TempDir(), "clone")
	got, e := captureStageGit(context.Background(), src, files.Path, clone)
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Repositories) != 2 {
		t.Fatalf("repositories=%d", len(got.Repositories))
	}
	capturedChild := filepath.Join(clone, "child")
	if fixtureGit(t, capturedChild, "show", ":file") != "module staged" || fixtureGit(t, capturedChild, "status", "--porcelain=v1") != before {
		t.Fatal("dirty submodule index/work state lost")
	}
	if st, e := os.Lstat(filepath.Join(capturedChild, ".git")); e != nil || !st.IsDir() {
		t.Fatalf("submodule not independent: %v", e)
	}
}

func TestLocalStageGitSourceAlternatesMaterialized(t *testing.T) {
	original := t.TempDir()
	fixtureGit(t, original, "init", "--template=", "--initial-branch=main")
	if e := os.WriteFile(filepath.Join(original, "file"), []byte("borrowed\n"), 0644); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, original, "add", "file")
	fixtureGit(t, original, "commit", "-m", "base")
	parent := t.TempDir()
	src := filepath.Join(parent, "shared")
	fixtureGit(t, parent, "clone", "--shared", "--no-hardlinks", original, src)
	files, e := captureStageFiles(context.Background(), src, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer files.Close()
	clone := filepath.Join(t.TempDir(), "clone")
	_, e = captureStageGit(context.Background(), src, files.Path, clone)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(clone, ".git", "objects", "info", "alternates")); !os.IsNotExist(e) {
		t.Fatalf("alternate retained: %v", e)
	}
	if fixtureGit(t, clone, "show", "HEAD:file") != "borrowed" {
		t.Fatal("borrowed full graph not materialized")
	}
}

func TestLocalStageGitPreservesConflictStagesFlagsAndLocallyCompleteShallow(t *testing.T) {
	for _, mode := range []string{"conflict", "split_index", "intent_to_add", "locally_complete_shallow"} {
		t.Run(mode, func(t *testing.T) {
			src := t.TempDir()
			fixtureGit(t, src, "init", "--template=", "--initial-branch=main")
			if err := os.WriteFile(filepath.Join(src, "file"), []byte("base\n"), 0644); err != nil {
				t.Fatal(err)
			}
			fixtureGit(t, src, "add", "file")
			fixtureGit(t, src, "commit", "-m", "base")
			first := fixtureGit(t, src, "rev-parse", "HEAD")
			if err := os.WriteFile(filepath.Join(src, "file"), []byte("second\n"), 0644); err != nil {
				t.Fatal(err)
			}
			fixtureGit(t, src, "add", "file")
			fixtureGit(t, src, "commit", "-m", "second")
			switch mode {
			case "conflict":
				old := fixtureGit(t, src, "rev-parse", first+":file")
				current := fixtureGit(t, src, "rev-parse", "HEAD:file")
				cmd := exec.Command("git", "-C", src, "update-index", "--index-info")
				cmd.Stdin = strings.NewReader("0 " + strings.Repeat("0", 40) + "\tfile\n100644 " + old + " 1\tfile\n100644 " + old + " 2\tfile\n100644 " + current + " 3\tfile\n")
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("fixture conflict: %v %s", err, output)
				}
			case "split_index":
				fixtureGit(t, src, "update-index", "--split-index")
			case "intent_to_add":
				if err := os.WriteFile(filepath.Join(src, "new"), []byte("intent\n"), 0644); err != nil {
					t.Fatal(err)
				}
				fixtureGit(t, src, "add", "--intent-to-add", "new")
			case "locally_complete_shallow":
				if err := os.WriteFile(filepath.Join(src, ".git", "shallow"), []byte(fixtureGit(t, src, "rev-parse", "HEAD")+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := fixtureGit(t, src, "status", "--porcelain=v1")
			stages := fixtureGit(t, src, "ls-files", "--stage")
			files, err := captureStageFiles(context.Background(), src, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			clone := filepath.Join(t.TempDir(), "clone")
			_, err = captureStageGit(context.Background(), src, files.Path, clone)
			if err != nil {
				t.Fatal(err)
			}
			if fixtureGit(t, clone, "status", "--porcelain=v1") != before || fixtureGit(t, clone, "ls-files", "--stage") != stages {
				t.Fatal("raw index categories/stages changed")
			}
			if mode == "locally_complete_shallow" && fixtureGit(t, clone, "cat-file", "-t", first) != "commit" {
				t.Fatal("locally complete history truncated")
			}
		})
	}
}
