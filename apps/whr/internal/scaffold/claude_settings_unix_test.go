//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

func TestInitClaudeSettingsSpecialTargets(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "directory", "parent_file", "parent_symlink"} {
		for _, overwrite := range []bool{false, true} {
			for _, dry := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/overwrite=%v/dry=%v", kind, overwrite, dry), func(t *testing.T) {
					dir := fixture(t)
					parent := filepath.Join(dir, ".claude")
					target := filepath.Join(parent, "settings.json")
					outsideDir := t.TempDir()
					outside := filepath.Join(outsideDir, "settings.json")
					owner := []byte("outside owner settings sentinel\n")
					if err := os.WriteFile(outside, owner, 0640); err != nil {
						t.Fatal(err)
					}
					stamp := time.Unix(1700000000, 0)
					if err := os.Chtimes(outside, stamp, stamp); err != nil {
						t.Fatal(err)
					}
					protected := target
					if kind == "parent_file" {
						protected = parent
						if err := os.WriteFile(parent, []byte("owner\n"), 0600); err != nil {
							t.Fatal(err)
						}
					} else if kind == "parent_symlink" {
						protected = parent
						if err := os.Symlink(outsideDir, parent); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.Mkdir(parent, 0755); err != nil {
							t.Fatal(err)
						}
						var err error
						switch kind {
						case "symlink":
							err = os.Symlink(outside, target)
						case "fifo":
							err = unix.Mkfifo(target, 0600)
						case "directory":
							err = os.Mkdir(target, 0755)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					before, err := os.Lstat(protected)
					if err != nil {
						t.Fatal(err)
					}
					outsideBefore, err := os.Stat(outside)
					if err != nil {
						t.Fatal(err)
					}
					rootBefore, err := os.Stat(dir)
					if err != nil {
						t.Fatal(err)
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
					o := Options{NonInteractive: true, Yes: true, DryRun: dry}
					if overwrite {
						o.Overwrite = []string{claudeSettingsPath}
					}
					p, err := Build(s, o)
					if overwrite {
						assertClaudeRefusal(t, err, exitcode.Conflict, "Claude settings target must be a regular file under a real .claude directory")
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if a := claudeAction(t, p); a.Action != "left_unchanged" || a.Applied {
							t.Fatalf("action=%+v", a)
						}
						parents := 0
						for _, a := range p.Preview().Actions {
							if a.Path == ".claude" {
								parents++
								if a.Action != "reuse" || a.Kind != "directory" || a.Applied {
									t.Fatalf("real parent not reused: %+v", a)
								}
							}
						}
						wantParents := 1
						if kind == "parent_file" || kind == "parent_symlink" {
							wantParents = 0
						}
						if parents != wantParents {
							t.Fatalf("parent rows=%d want %d", parents, wantParents)
						}
						steps := 0
						for _, step := range p.Preview().NextSteps {
							if strings.Contains(step, "attribution.sessionUrl: false") && strings.Contains(step, "--overwrite .claude/settings.json") && strings.Contains(step, "whole file") {
								steps++
							}
						}
						if steps != 1 || !strings.Contains(p.HumanPreview(), "left_unchanged file .claude/settings.json\n") {
							t.Fatal("special target lost remediation or output")
						}
						view, err := p.Apply(context.Background())
						if err != nil {
							t.Fatal(err)
						}
						for _, a := range view.Actions {
							if a.Path == claudeSettingsPath && (a.Action != "left_unchanged" || a.Applied) {
								t.Fatalf("special target applied: %+v", a)
							}
						}
					}
					after, err := os.Lstat(protected) // Never open or read a FIFO.
					if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
						t.Fatalf("protected target identity/time/mode changed: %v", err)
					}
					if kind == "symlink" || kind == "parent_symlink" {
						want := outside
						if kind == "parent_symlink" {
							want = outsideDir
						}
						if link, err := os.Readlink(protected); err != nil || link != want {
							t.Fatalf("link destination changed: %q / %v", link, err)
						}
					}
					if kind == "parent_file" {
						if data, err := os.ReadFile(parent); err != nil || string(data) != "owner\n" {
							t.Fatalf("parent file changed: %v", err)
						}
					}
					outsideAfter, err := os.Stat(outside)
					if err != nil || !os.SameFile(outsideBefore, outsideAfter) || outsideBefore.Mode() != outsideAfter.Mode() || !outsideBefore.ModTime().Equal(outsideAfter.ModTime()) {
						t.Fatalf("outside file metadata changed: %v", err)
					}
					if data, err := os.ReadFile(outside); err != nil || !bytes.Equal(data, owner) {
						t.Fatalf("outside file bytes changed: %v", err)
					}
					if overwrite || dry {
						after, err := os.Stat(dir)
						if err != nil || !os.SameFile(rootBefore, after) || !rootBefore.ModTime().Equal(after.ModTime()) {
							t.Fatalf("refusal/dry-run changed workspace: %v", err)
						}
						if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
							t.Fatalf("refusal/dry-run wrote manifest: %v", err)
						}
					}
				})
			}
		}
	}
}
