//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestInitApplyHostileTargetsRefused(t *testing.T) {
	for _, kind := range []string{"file-link", "parent-link", "fifo", "root-link"} {
		t.Run(kind, func(t *testing.T) {
			dir := fixture(t)
			p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
			other := t.TempDir()
			marker := filepath.Join(other, "marker")
			if err := os.WriteFile(marker, []byte("untouched"), 0644); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file-link":
				name := filepath.Join(dir, ".devcontainer/devcontainer.json")
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(marker, name); err != nil {
					t.Fatal(err)
				}
			case "parent-link":
				name := filepath.Join(dir, ".devcontainer")
				if err := os.Rename(name, filepath.Join(dir, "retained")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, name); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(filepath.Join(dir, "workharbor.json"), 0600); err != nil {
					t.Fatal(err)
				}
			case "root-link":
				moved := dir + "-moved"
				if err := os.Rename(dir, moved); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(dir); _ = os.Rename(moved, dir) })
				if err := os.Symlink(moved, dir); err != nil {
					t.Fatal(err)
				}
			}
			_, err := p.Apply(context.Background())
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("unsafe target not refused: %v", err)
			}
			raw, _ := os.ReadFile(marker)
			if string(raw) != "untouched" {
				t.Fatal("followed link into foreign target")
			}
		})
	}
}
