// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

func TestLocalPromotionMissingStateNeverRepairsPreview(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "missing", "state")
	_, err := openPromotionStore(root, "daemon", "unix:///fixture.sock")
	requireStageGitError(t, err, exitcode.Conflict, "promotion state is absent or symlinked")
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing preview repaired state: %v %v", entries, err)
	}
}
func TestLocalPromotionExistingStateLockAndNoTouch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	fixture, err := openStageStore(root, "daemon", "unix:///fixture.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.path
	fixture.Close()
	before, err := os.Stat(filepath.Join(path, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := openPromotionStore(root, "daemon", "unix:///fixture.sock")
	if err != nil {
		t.Fatal(err)
	}
	_, err = openPromotionStore(root, "daemon", "unix:///fixture.sock")
	requireStageGitError(t, err, exitcode.Conflict, "promotion operation lock is held")
	s.Close()
	after, err := os.Stat(filepath.Join(path, "lock"))
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || after.Size() != before.Size() {
		t.Fatal("preview touched persistent lock bytes or identity")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 1 || entries[0].Name() != "lock" {
		t.Fatalf("preview created persistent artifacts: %v %v", entries, err)
	}
}
func TestLocalPromotionStateUnsafePathsRefuseBeforeRead(t *testing.T) {
	for _, kind := range []string{"root_symlink", "daemon_symlink", "lock_symlink", "lock_fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			fixture, err := openStageStore(root, "daemon", "unix:///fixture.sock")
			if err != nil {
				t.Fatal(err)
			}
			path := fixture.path
			fixture.Close()
			reason := "promotion state is absent or symlinked"
			switch kind {
			case "root_symlink":
				moved := root + "-moved"
				if err = os.Rename(root, moved); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(moved, root); err != nil {
					t.Fatal(err)
				}
			case "daemon_symlink":
				moved := path + "-moved"
				if err = os.Rename(path, moved); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(moved, path); err != nil {
					t.Fatal(err)
				}
			case "lock_symlink":
				if err = os.Remove(filepath.Join(path, "lock")); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "owner-file")
				if err = os.WriteFile(outside, []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(outside, filepath.Join(path, "lock")); err != nil {
					t.Fatal(err)
				}
				reason = "promotion lock is unavailable or symlinked"
			case "lock_fifo":
				if err = os.Remove(filepath.Join(path, "lock")); err != nil {
					t.Fatal(err)
				}
				if err = unix.Mkfifo(filepath.Join(path, "lock"), 0600); err != nil {
					t.Fatal(err)
				}
				reason = "promotion lock ownership or mode"
			}
			_, err = openPromotionStore(root, "daemon", "unix:///fixture.sock")
			requireStageGitError(t, err, exitcode.Conflict, reason)
		})
	}
}
