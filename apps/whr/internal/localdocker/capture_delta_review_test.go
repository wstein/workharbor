// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStageGitSubmoduleUnderReadOnlyDirectory(t *testing.T) {
	src, child := captureRegressionRepo(t), captureRegressionRepo(t)
	fixtureGit(t, src, "-c", "protocol.file.allow=always", "submodule", "add", child, "ro/module")
	fixtureGit(t, src, "commit", "-am", "nested module")
	ro := filepath.Join(src, "ro")
	if err := os.Chmod(ro, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0700) })
	files := captureRegressionFiles(t, src)
	clone := filepath.Join(capturePhysicalTemp(t), "clone")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(clone, "ro"), 0700) })
	capture, err := captureStageGit(context.Background(), src, files.Path, clone)
	if err != nil {
		t.Fatalf("read-only ancestor refused: %v", err)
	}
	if len(capture.Repositories) != 2 || fixtureGit(t, filepath.Join(clone, "ro/module"), "show", "HEAD:file") != "base" {
		t.Fatal("nested repository/history missing")
	}
	st, err := os.Stat(filepath.Join(clone, "ro"))
	if err != nil || st.Mode().Perm() != 0555 {
		t.Fatalf("read-only ancestor mode lost: %v %v", st, err)
	}
}
func TestLocalStageGitObjectAuditRejectsUnsafeNodes(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := capturePhysicalTemp(t)
			path := filepath.Join(root, "foreign")
			reason := "Git object administration is symlinked"
			var err error
			if kind == "symlink" {
				err = os.Symlink("missing", path)
			} else {
				err = unix.Mkfifo(path, 0600)
				reason = "Git object administration file type unsupported"
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = stageObjectStoreBinding(context.Background(), root)
			requireCaptureConflict(t, err, reason)
		})
	}
}
func TestLocalStageGitObjectAuditRechecksReplacedIdentity(t *testing.T) {
	parent := capturePhysicalTemp(t)
	root := filepath.Join(parent, "objects")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "info"), 0700); err != nil {
		t.Fatal(err)
	}
	audit := &stageObjectAudit{checked: map[string]bool{}}
	ctx := context.WithValue(context.Background(), stageObjectAuditKey{}, audit)
	if _, err := stageObjectStoreBinding(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "unexpected"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := stageObjectStoreBinding(ctx, root)
	requireCaptureConflict(t, err, "Git object administration file type unsupported")
}
func TestLocalStageGitExcludeAdministrationBoundaries(t *testing.T) {
	for _, kind := range []string{"symlink", "N", "N_plus_one"} {
		t.Run(kind, func(t *testing.T) {
			src := captureRegressionRepo(t)
			info := filepath.Join(src, ".git/info")
			if err := os.MkdirAll(info, 0700); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Rename(info, info+"-actual"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(info+"-actual", info); err != nil {
					t.Fatal(err)
				}
			} else {
				size := 256 << 10
				if kind == "N_plus_one" {
					size++
				}
				if err := os.WriteFile(filepath.Join(info, "exclude"), bytes.Repeat([]byte{'#'}, size), 0600); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := readStageGitSnapshotContext(context.Background(), src)
			if kind == "symlink" {
				requireCaptureConflict(t, err, "Git exclude administration is symlinked or unavailable")
			} else if kind == "N_plus_one" {
				requireCaptureConflict(t, err, "Git administration file type or quota")
			} else if err != nil || len(snapshot.Exclude) != 256<<10 {
				t.Fatalf("exact exclude bound refused: %d %v", len(snapshot.Exclude), err)
			}
		})
	}
}
func TestLocalStageGitExcludeDriftChangesBinding(t *testing.T) {
	src := captureRegressionRepo(t)
	info := filepath.Join(src, ".git/info")
	if err := os.MkdirAll(info, 0700); err != nil {
		t.Fatal(err)
	}
	exclude := filepath.Join(info, "exclude")
	if err := os.WriteFile(exclude, []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files := captureRegressionFiles(t, src)
	changed := false
	ctx := captureControl(context.Background(), nil, func(phase, _ string) {
		if phase == "repositories-copied" {
			changed = true
			if err := os.WriteFile(exclude, []byte("second\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}, nil)
	_, err := captureStageGit(ctx, src, files.Path, filepath.Join(capturePhysicalTemp(t), "clone"))
	requireCaptureConflict(t, err, "source Git state changed during recursive capture")
	if !changed {
		t.Fatal("exclude drift checkpoint not reached")
	}
}
func TestLocalStageGitReaderSharesPlatformFilemode(t *testing.T) {
	src := captureRegressionRepo(t)
	if err := os.Chmod(filepath.Join(src, "file"), 0700); err != nil {
		t.Fatal(err)
	}
	files := captureRegressionFiles(t, src)
	forced := false
	ctx := captureControl(context.Background(), nil, nil, func(cmd *exec.Cmd) {
		for i, arg := range cmd.Args {
			if arg == "--file" && i+1 < len(cmd.Args) && strings.HasSuffix(cmd.Args[i+1], "config-input") {
				path := cmd.Args[i+1]
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				// Only the clone-generated platform config has its default origin remote.
				if bytes.Contains(data, []byte("[remote \"origin\"]")) {
					forced = true
					data = bytes.ReplaceAll(data, []byte("filemode = true"), []byte("filemode = false"))
					if err = os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	})
	capture, err := captureStageGit(ctx, src, files.Path, filepath.Join(capturePhysicalTemp(t), "clone"))
	if err != nil {
		t.Fatalf("reader/candidate platform status diverged: %v", err)
	}
	if !forced || capture.Dirty {
		t.Fatalf("platform filemode fixture ineffective: forced=%v dirty=%v", forced, capture.Dirty)
	}
}
