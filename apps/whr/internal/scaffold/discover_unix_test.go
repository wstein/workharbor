//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

func TestInitDiscoverySpecialFileRefused(t *testing.T) {
	for _, raced := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing FIFO", true: "FIFO after inspection"}[raced], func(t *testing.T) {
			dir := fixture(t)
			name := ".devcontainer/devcontainer.json"
			target := filepath.Join(dir, name)
			replace := func() {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if !raced {
				replace()
			}
			_, err := discover(context.Background(), dir, func(path string) {
				if raced && path == name {
					replace()
				}
			})
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("special target must be refused without reading: %v", err)
			}
		})
	}
}

// TestInitDiscoveryRecheckRefusesChangeAfterRead changes an already-read target
// while a later target is being opened. Only the final recheck can notice it.
func TestInitDiscoveryRecheckRefusesChangeAfterRead(t *testing.T) {
	const manifest = `{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`
	changes := map[string]func(t *testing.T, dir, target string){
		"mode": func(t *testing.T, dir, target string) {
			if err := os.Chmod(target, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"identity": func(t *testing.T, dir, target string) {
			old, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			copyName := filepath.Join(dir, "same-bytes")
			if err := os.WriteFile(copyName, []byte(manifest), old.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(copyName, old.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(copyName, old.ModTime(), old.ModTime()); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(copyName, target); err != nil {
				t.Fatal(err)
			}
		},
		"size": func(t *testing.T, dir, target string) {
			old, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte(manifest+"\n"), old.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(target, old.ModTime(), old.ModTime()); err != nil {
				t.Fatal(err)
			}
		},
		"mtime": func(t *testing.T, dir, target string) {
			later := time.Now().Add(time.Hour)
			if err := os.Chtimes(target, later, later); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			target := filepath.Join(dir, "workharbor.json")
			if err := os.WriteFile(target, []byte(manifest), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(target, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Discover(context.Background(), dir); err != nil {
				t.Fatalf("unmodified fixture must be accepted: %v", err)
			}
			once := false
			_, err := discover(context.Background(), dir, func(at string) {
				if at == ".devcontainer/devcontainer.json" && !once {
					once = true
					change(t, dir, target)
				}
			})
			if !once {
				t.Fatal("hook never ran")
			}
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("change after read was not refused: %v", err)
			}
		})
	}
}

// TestInitDiscoveryRefusesRootReplacedAfterReads swaps the selected directory
// name after the held handle was validated; the final name lookup must refuse.
func TestInitDiscoveryRefusesRootReplacedAfterReads(t *testing.T) {
	dir := fixture(t)
	once := false
	_, err := discover(context.Background(), dir, func(at string) {
		if at != ".devcontainer/devcontainer.json" || once {
			return
		}
		once = true
		if err := os.Rename(dir, dir+"-moved"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir); _ = os.Rename(dir+"-moved", dir) })
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	})
	if !once {
		t.Fatal("hook never ran")
	}
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("replaced root name was not refused: %v", err)
	}
}
