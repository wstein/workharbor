// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func applyProposal(t *testing.T, dir string, o Options) *Proposal {
	t.Helper()
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestInitFileChangedSincePreview(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"changed"}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := p.Apply(context.Background())
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("stale preview must refuse68: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
		t.Fatal("stale apply wrote manifest")
	}
}
func TestInitApplyCreatesAndValidates(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	result, err := p.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if string(raw) != string(p.Files()["workharbor.json"]) {
		t.Fatal("written bytes differ from preview")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects/.gitkeep")); err != nil {
		t.Fatal(err)
	}
	if !result.Validation.Valid || result.GitChecks != "not_run" {
		t.Fatal("missing final Git-free validation")
	}
	applied := 0
	for _, a := range result.Actions {
		if a.Applied {
			applied++
		}
	}
	if applied != 3 {
		t.Fatalf("applied actions=%d, want manifest, projects and placeholder", applied)
	}
}
func TestInitFailureInjectionRollback(t *testing.T) {
	events := []string{}
	probe := applyProposal(t, fixture(t), Options{NonInteractive: true, Yes: true})
	if _, err := probe.apply(context.Background(), func(event string) error { events = append(events, event); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%t", event, cancelled), func(t *testing.T) {
				dir := fixture(t)
				before, _ := os.ReadFile(filepath.Join(dir, ".devcontainer/devcontainer.json"))
				p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				_, err := p.apply(ctx, func(at string) error {
					if at == event {
						if cancelled {
							cancel()
							return nil
						}
						return syscall.ENOSPC
					}
					return nil
				})
				want := 74
				if cancelled {
					want = 72
				}
				if int(exitcode.FromError(err)) != want {
					t.Fatalf("exit=%v want%d", err, want)
				}
				names, _ := os.ReadDir(dir)
				if len(names) != 1 || names[0].Name() != ".devcontainer" {
					t.Fatalf("rollback left entries: %v", names)
				}
				after, _ := os.ReadFile(filepath.Join(dir, ".devcontainer/devcontainer.json"))
				if !bytes.Equal(before, after) {
					t.Fatal("reused bytes changed")
				}
			})
		}
	}
}
func TestInitRollbackRestoresOverwriteModeAndBytes(t *testing.T) {
	dir := fixture(t)
	old := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
	name := filepath.Join(dir, "workharbor.json")
	if err := os.WriteFile(name, old, 0600); err != nil {
		t.Fatal(err)
	}
	value := "changed"
	p := applyProposal(t, dir, Options{Name: &value, NonInteractive: true, Yes: true, Overwrite: []string{"workharbor.json"}})
	_, err := p.apply(context.Background(), func(at string) error {
		if at == "after:workharbor.json" {
			return syscall.ENOSPC
		}
		return nil
	})
	if int(exitcode.FromError(err)) != 74 {
		t.Fatalf("rollback error: %v", err)
	}
	after, _ := os.ReadFile(name)
	info, _ := os.Stat(name)
	if !bytes.Equal(after, old) || info.Mode().Perm() != 0600 {
		t.Fatalf("restore bytes/mode wrong: %s %v", after, info.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("rollback leftovers: %v", entries)
	}
}
func TestInitRollbackIncompleteExits73(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	alien := []byte("another writer owns this inode")
	_, err := p.apply(context.Background(), func(at string) error {
		if at == "after:workharbor.json" {
			// Keep the old inode allocated: a replacement cannot reuse its number.
			if e := os.Rename(filepath.Join(dir, "workharbor.json"), filepath.Join(dir, "retained")); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, "workharbor.json"), alien, 0644); e != nil {
				t.Fatal(e)
			}
			return syscall.ENOSPC
		}
		return nil
	})
	if int(exitcode.FromError(err)) != 73 || !strings.Contains(err.Error(), "workharbor.json") || !strings.Contains(err.Error(), "recover") {
		t.Fatalf("missing partial recovery: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if !bytes.Equal(raw, alien) {
		t.Fatal("rollback deleted another writer's replacement")
	}
	if _, err := Discover(context.Background(), dir); int(exitcode.FromError(err)) != 68 {
		t.Fatalf("leftover marker did not refuse next init: %v", err)
	}
}
func TestInitApplyNoOpAndDryRunPreserveFiles(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			dir := fixture(t)
			if !dry {
				if _, err := applyProposal(t, dir, Options{NonInteractive: true, Yes: true}).Apply(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			before, err := Discover(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			root, _ := os.Stat(dir)
			p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true, DryRun: dry})
			result, err := p.Apply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			after, err := Discover(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			latest, _ := os.Stat(dir)
			if !os.SameFile(root, latest) || root.ModTime() != latest.ModTime() || len(before.Entries) != len(after.Entries) {
				t.Fatal("no-op/dry-run changed tree")
			}
			for name, entry := range before.Entries {
				now := after.Entries[name]
				if !os.SameFile(entry.Info, now.Info) || entry.Mode != now.Mode || entry.Info.ModTime() != now.Info.ModTime() || !bytes.Equal(entry.Data, now.Data) {
					t.Fatalf("changed %s", name)
				}
			}
			for _, a := range result.Actions {
				if a.Applied {
					t.Fatal("no-op/dry-run marked applied")
				}
			}
		})
	}
}
func TestInitPathAppearingAfterPreviewRefused(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	_, err := p.apply(context.Background(), func(at string) error {
		if at == "before:workharbor.json" {
			return os.WriteFile(filepath.Join(dir, "workharbor.json"), []byte("foreign"), 0644)
		}
		return nil
	})
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("appearing path not refused: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if string(raw) != "foreign" {
		t.Fatal("replaced concurrent file")
	}
}
func TestInitOverwritePreservesModeBeyondUmask(t *testing.T) {
	dir := fixture(t)
	name := filepath.Join(dir, "workharbor.json")
	raw := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0666); err != nil {
		t.Fatal(err)
	}
	value := "new"
	p := applyProposal(t, dir, Options{Name: &value, NonInteractive: true, Yes: true, Overwrite: []string{"workharbor.json"}})
	want := p.Files()["workharbor.json"]
	if bytes.Equal(want, raw) {
		t.Fatal("overwrite proposal did not change input bytes")
	}
	if _, err := p.Apply(context.Background()); err != nil {
		t.Fatalf("ordinary overwrite failed: %v", err)
	}
	written, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, want) {
		t.Fatal("overwrite did not write approved proposal bytes")
	}
	info, err := os.Stat(name)
	if err != nil || info.Mode().Perm() != 0666 {
		t.Fatalf("mode not preserved: %v %v", info, err)
	}
}
func TestInitApplyRejectsModeChange(t *testing.T) {
	for _, name := range []string{".", ".devcontainer", ".devcontainer/devcontainer.json"} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
			if err := os.Chmod(filepath.Join(dir, name), 0700); err != nil {
				t.Fatal(err)
			}
			_, err := p.Apply(context.Background())
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("mode change not refused: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
				t.Fatal("mode conflict wrote manifest")
			}
		})
	}
}
func TestInitOverwriteTemporaryReplacementRefused(t *testing.T) {
	dir := fixture(t)
	name := filepath.Join(dir, "workharbor.json")
	old := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
	if err := os.WriteFile(name, old, 0644); err != nil {
		t.Fatal(err)
	}
	value := "changed"
	p := applyProposal(t, dir, Options{Name: &value, NonInteractive: true, Yes: true, Overwrite: []string{"workharbor.json"}})
	reached := false
	_, err := p.apply(context.Background(), func(at string) error {
		if at == "rename:workharbor.json" {
			reached = true
			temp := filepath.Join(dir, ".whr-init-tmp-00000001")
			if e := os.Rename(temp, filepath.Join(dir, "retained-temp")); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(temp, []byte("foreign replacement"), 0644); e != nil {
				t.Fatal(e)
			}
		}
		return nil
	})
	if !reached {
		t.Fatal("rename control not exercised")
	}
	raw, _ := os.ReadFile(name)
	if int(exitcode.FromError(err)) != 73 || !bytes.Equal(raw, old) {
		t.Fatalf("temporary substitution reached target or lost partial report: %v bytes=%s", err, raw)
	}
	foreign, _ := os.ReadFile(filepath.Join(dir, ".whr-init-tmp-00000001"))
	if string(foreign) != "foreign replacement" {
		t.Fatal("removed replacement temp")
	}
}
func TestInitOverwriteFailureInjectionEveryStep(t *testing.T) {
	setup := func(t *testing.T) (string, []byte, *Proposal) {
		t.Helper()
		dir := fixture(t)
		old := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
		if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), old, 0600); err != nil {
			t.Fatal(err)
		}
		value := "changed"
		return dir, old, applyProposal(t, dir, Options{Name: &value, NonInteractive: true, Yes: true, Overwrite: []string{"workharbor.json"}})
	}
	_, _, probe := setup(t)
	var events []string
	if _, err := probe.apply(context.Background(), func(at string) error { events = append(events, at); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		t.Run(event, func(t *testing.T) {
			dir, old, p := setup(t)
			_, err := p.apply(context.Background(), func(at string) error {
				if at == event {
					return syscall.ENOSPC
				}
				return nil
			})
			if int(exitcode.FromError(err)) != 74 {
				t.Fatalf("wrong failure or failed rollback: %v", err)
			}
			raw, e := os.ReadFile(filepath.Join(dir, "workharbor.json"))
			if e != nil || !bytes.Equal(raw, old) {
				t.Fatal("old bytes not restored")
			}
			info, _ := os.Stat(filepath.Join(dir, "workharbor.json"))
			if info.Mode().Perm() != 0600 {
				t.Fatal("old mode not restored")
			}
			names, _ := os.ReadDir(dir)
			if len(names) != 2 {
				t.Fatalf("rollback leftovers: %v", names)
			}
		})
	}
}
func TestInitOverwriteTargetChangeBeforeRenameRefused(t *testing.T) {
	dir := fixture(t)
	name := filepath.Join(dir, "workharbor.json")
	old := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
	if err := os.WriteFile(name, old, 0600); err != nil {
		t.Fatal(err)
	}
	value := "changed"
	p := applyProposal(t, dir, Options{Name: &value, NonInteractive: true, Yes: true, Overwrite: []string{"workharbor.json"}})
	_, err := p.apply(context.Background(), func(at string) error {
		if at == "rename:workharbor.json" {
			return os.WriteFile(name, []byte("foreign edit"), 0600)
		}
		return nil
	})
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("immediate target change not refused: %v", err)
	}
	raw, _ := os.ReadFile(name)
	if string(raw) != "foreign edit" {
		t.Fatal("overwrote concurrent edit")
	}
}
func TestInitPermissionFailureNamesTarget(t *testing.T) {
	dir := fixture(t)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	_, err := p.Apply(context.Background())
	if int(exitcode.FromError(err)) != 68 || !strings.Contains(err.Error(), ".whr-init-tmp-journal") {
		t.Fatalf("permission denial must name refused target: %v", err)
	}
}
func TestInitRollbackRefusesReplacedDirectory(t *testing.T) {
	dir := fixture(t)
	other := t.TempDir()
	marker := filepath.Join(other, "marker")
	if err := os.WriteFile(marker, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	_, err := p.apply(context.Background(), func(at string) error {
		if at == "after:projects" {
			if e := os.Rename(filepath.Join(dir, "projects"), filepath.Join(dir, "retained-projects")); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(other, filepath.Join(dir, "projects")); e != nil {
				t.Fatal(e)
			}
			return context.Canceled
		}
		return nil
	})
	if int(exitcode.FromError(err)) != 73 || !strings.Contains(err.Error(), "projects") {
		t.Fatalf("replaced directory needs partial recovery: %v", err)
	}
	raw, _ := os.ReadFile(marker)
	if string(raw) != "untouched" {
		t.Fatal("rollback followed replaced parent")
	}
	info, _ := os.Lstat(filepath.Join(dir, "projects"))
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("rollback removed foreign link")
	}
}
