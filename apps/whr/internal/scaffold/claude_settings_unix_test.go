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

func TestInitClaudeSettingsSpecialTargets(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "directory", "parent_file", "parent_symlink"} {
		for _, overwrite := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "_keep", true: "_overwrite"}[overwrite], func(t *testing.T) {
				dir := fixture(t)
				parent := filepath.Join(dir, ".claude")
				target := filepath.Join(parent, "settings.json")
				if kind == "parent_file" {
					if err := os.WriteFile(parent, []byte("owner"), 0644); err != nil {
						t.Fatal(err)
					}
				} else if kind == "parent_symlink" {
					if err := os.Symlink(t.TempDir(), parent); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(parent, 0755); err != nil {
						t.Fatal(err)
					}
					var err error
					switch kind {
					case "symlink":
						err = os.Symlink(filepath.Join(t.TempDir(), "absent"), target)
					case "fifo":
						err = unix.Mkfifo(target, 0600)
					case "directory":
						err = os.Mkdir(target, 0755)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				opened := false
				s, err := discover(context.Background(), dir, func(name string) {
					if name == claudeSettingsPath || name == ".claude" && (kind == "parent_file" || kind == "parent_symlink") {
						opened = true
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				if opened {
					t.Fatal("special target opened")
				}
				o := Options{NonInteractive: true, Yes: true}
				if overwrite {
					o.Overwrite = []string{claudeSettingsPath}
				}
				p, err := Build(s, o)
				if overwrite {
					if int(exitcode.FromError(err)) != 68 {
						t.Fatalf("special overwrite=%v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if a := claudeAction(t, p); a.Action != "left_unchanged" || a.Applied {
					t.Fatalf("action=%+v", a)
				}
				if kind == "parent_file" || kind == "parent_symlink" {
					for _, a := range p.Preview().Actions {
						if a.Path == ".claude" {
							t.Fatal("unsafe parent emitted row")
						}
					}
				}
				if _, err := p.Apply(context.Background()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
