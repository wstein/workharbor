// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capturePhysicalTemp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func captureRegressionRepo(t *testing.T) string {
	t.Helper()
	src := capturePhysicalTemp(t)
	fixtureGit(t, src, "init", "--template=", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(src, "file"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, src, "add", "file")
	fixtureGit(t, src, "commit", "-m", "base")
	return src
}
func captureRegressionFiles(t *testing.T, src string) *capturedStageFiles {
	t.Helper()
	files, err := captureStageFiles(context.Background(), src, capturePhysicalTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	return files
}
func TestLocalStageGitFailedSubmoduleLeavesNoClone(t *testing.T) {
	child, src := captureRegressionRepo(t), captureRegressionRepo(t)
	fixtureGit(t, src, "-c", "protocol.file.allow=always", "submodule", "add", child, "module")
	fixtureGit(t, src, "commit", "-am", "module")
	sub := filepath.Join(src, "module")
	fixtureGit(t, sub, "update-ref", "refs/custom/x", fixtureGit(t, sub, "rev-parse", "HEAD"))
	fixtureGit(t, sub, "symbolic-ref", "HEAD", "refs/custom/x")
	files := captureRegressionFiles(t, src)
	parent := capturePhysicalTemp(t)
	clone := filepath.Join(parent, "clone")
	_, err := captureStageGit(context.Background(), src, files.Path, clone)
	requireCaptureConflict(t, err, "Git HEAD symbolic identity unsupported")
	entries, e := os.ReadDir(parent)
	if e != nil || len(entries) != 0 {
		t.Fatalf("failed capture retained clone/private state: %v %v", entries, e)
	}
}
func TestLocalStageGitReadOnlyWorkingDirectories(t *testing.T) {
	src := captureRegressionRepo(t)
	ro := filepath.Join(src, "ro")
	if err := os.Mkdir(ro, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ro, "nested"), []byte("kept"), 0444); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, src, "add", "ro")
	fixtureGit(t, src, "commit", "-m", "readonly")
	if err := os.Chmod(ro, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0700) })
	files := captureRegressionFiles(t, src)
	clone := filepath.Join(capturePhysicalTemp(t), "clone")
	if _, err := captureStageGit(context.Background(), src, files.Path, clone); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(clone, "ro"), 0700) })
	b, err := os.ReadFile(filepath.Join(clone, "ro", "nested"))
	if err != nil || string(b) != "kept" {
		t.Fatalf("readonly copy: %q %v", b, err)
	}
	st, err := os.Stat(filepath.Join(clone, "ro"))
	if err != nil || st.Mode().Perm() != 0555 {
		t.Fatalf("directory mode: %v %v", st, err)
	}
}
func TestLocalStageGitCopyFailureIsClassifiedWithoutHostPath(t *testing.T) {
	src, dst := capturePhysicalTemp(t), capturePhysicalTemp(t)
	for _, dir := range []string{src, dst} {
		if err := os.WriteFile(filepath.Join(dir, "host\nCANARY"), []byte("value"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := copyStageWorkingSkip(src, dst, nil)
	requireCaptureConflict(t, err, "captured working output unavailable")
	if strings.Contains(err.Error(), dst) || strings.Contains(err.Error(), "CANARY") {
		t.Fatal("host pathname leaked")
	}
}
func TestLocalStageGitSymlinkedTemporaryParent(t *testing.T) {
	src := captureRegressionRepo(t)
	files := captureRegressionFiles(t, src)
	parent := capturePhysicalTemp(t)
	link := filepath.Join(capturePhysicalTemp(t), "tmp-alias")
	if err := os.Symlink(parent, link); err != nil {
		t.Fatal(err)
	}
	sourceAlias := filepath.Join(capturePhysicalTemp(t), "source-alias")
	if err := os.Symlink(src, sourceAlias); err != nil {
		t.Fatal(err)
	}
	got, err := captureStageGit(context.Background(), sourceAlias, files.Path, filepath.Join(link, "clone"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != src || got.Clone != filepath.Join(parent, "clone") {
		t.Fatalf("unbound physical paths: %+v", got)
	}
}
func TestLocalStageGitInfoExcludeRetainsCleanStatus(t *testing.T) {
	src := captureRegressionRepo(t)
	exclude := []byte("# inert source exclusions\nscratch\n")
	if err := os.WriteFile(filepath.Join(src, ".git", "info", "exclude"), exclude, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "scratch"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, src, "status", "--porcelain"); got != "" {
		t.Fatalf("source not clean: %q", got)
	}
	files := captureRegressionFiles(t, src)
	clone := filepath.Join(capturePhysicalTemp(t), "clone")
	got, err := captureStageGit(context.Background(), src, files.Path, clone)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dirty || fixtureGit(t, clone, "status", "--porcelain") != "" {
		t.Fatal("inert info/exclude lost")
	}
	b, err := os.ReadFile(filepath.Join(clone, ".git", "info", "exclude"))
	if err != nil || string(b) != string(exclude) {
		t.Fatalf("exclude copy: %q %v", b, err)
	}
}
func TestLocalStageGitReftableRefusalNamesStorage(t *testing.T) {
	src := captureRegressionRepo(t)
	f, err := os.OpenFile(filepath.Join(src, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("\n[extensions]\nrefStorage = reftable\n"); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = readStageGitSnapshotContext(context.Background(), src, false)
	requireCaptureConflict(t, err, "Git reftable reference storage unsupported")
}
func TestLocalStageGitUnsafeRemoteFormsRefused(t *testing.T) {
	for _, value := range []string{"ext::sh -c touch% /tmp/x", "evil::x", "fd::17", "-oProxyCommand=x:y", "ssh://u:password@host/path"} {
		if stageCredentialFreeURL(value) {
			t.Errorf("unsafe remote retained: %q", value)
		}
	}
	for _, value := range []string{"git@[::1]:path", "/tmp/local::repo", "https://example.test/repo.git"} {
		if !stageCredentialFreeURL(value) {
			t.Errorf("safe remote refused: %q", value)
		}
	}
}
