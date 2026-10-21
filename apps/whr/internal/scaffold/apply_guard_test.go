//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

const guardManifest = `{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`

// A rollback must give the old bytes back with the old permission bits, not a
// fixed mode: 0640 is neither the 0600 journal mode nor the 0644 create mode.
func TestInitRollbackRestoresDistinctOverwriteMode(t *testing.T) {
	dir := fixture(t)
	name := filepath.Join(dir, "workharbor.json")
	if err := os.WriteFile(name, []byte(guardManifest), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0640); err != nil {
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
	info, statErr := os.Stat(name)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("rollback restored mode %v, want 0640", info.Mode().Perm())
	}
}

// Every action starts with a recheck of all observed inputs; a change made
// after the previous action must stop the transaction before the next write.
func TestInitApplyRechecksAllInputsBeforeEachAction(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	var events []string
	_, err := p.apply(context.Background(), func(at string) error {
		events = append(events, at)
		if at == "after:projects" {
			return os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(`{"image":"swapped"}`), 0644)
		}
		return nil
	})
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("changed input was not refused: %v", err)
	}
	for _, event := range events {
		if strings.HasPrefix(event, "write:projects/") {
			t.Fatalf("an action ran after the inputs changed: %v", events)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(dir, "projects")); !os.IsNotExist(statErr) {
		t.Fatalf("rollback left projects behind: %v", statErr)
	}
}

// A leftover temporary that appears between preview and apply must stop Apply
// before the first write; the discovery-time scan alone is not enough.
func TestInitApplyScansForTemporariesAtApplyTime(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	stale := filepath.Join(dir, ".whr-init-tmp-00000009")
	if err := os.WriteFile(stale, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := p.Apply(context.Background())
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("stale temporary was not refused at apply time: %v", err)
	}
	for _, name := range []string{"workharbor.json", "projects", ".whr-init-tmp-journal"} {
		if _, statErr := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("%s written despite a stale temporary: %v", name, statErr)
		}
	}
}

// The rerun branch validates the files on disk and reports that validation.
// Its report names the cleaned directory, not the spelling used for discovery.
func TestInitApplyNoOpValidatesDisk(t *testing.T) {
	dir := fixture(t)
	if _, err := applyProposal(t, dir, Options{NonInteractive: true, Yes: true}).Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := applyProposal(t, dir+"/.", Options{NonInteractive: true, Yes: true})
	result, err := p.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Validation == nil || !result.Validation.Valid || result.Validation.Directory != dir {
		t.Fatalf("no-op apply did not report the on-disk validation: %+v", result.Validation)
	}
	// A no-op still refuses an input changed since the preview.
	if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(context.Background()); int(exitcode.FromError(err)) != 68 {
		t.Fatalf("no-op apply accepted a changed manifest: %v", err)
	}
}

// createExclusive must never reuse an existing name nor follow a link.
func TestInitCreateExclusiveRefusesExistingAndLinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("existing", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("absent-target", filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"existing", "link", "dangling"} {
		f, err := createExclusive(root, name, 0644)
		if err == nil {
			f.Close()
			t.Fatalf("%s: exclusive creation succeeded", name)
		}
		if !errors.Is(err, os.ErrExist) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(dir, "existing"))
	if !bytes.Equal(got, []byte("keep")) {
		t.Fatalf("existing file was modified: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, "absent-target")); !os.IsNotExist(err) {
		t.Fatal("dangling link target was created")
	}
}

// Rollback does not run checkAll, so parent itself must refuse a selected
// directory name that now refers to a different directory than the held one.
func TestInitRollbackRefusesReplacedRootName(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	moved := dir + "-moved"
	t.Cleanup(func() { _ = os.RemoveAll(dir); _ = os.Rename(moved, dir) })
	_, err := p.apply(context.Background(), func(at string) error {
		if at == "after:projects" {
			if e := os.Rename(dir, moved); e != nil {
				t.Fatal(e)
			}
			if e := os.Mkdir(dir, 0755); e != nil {
				t.Fatal(e)
			}
			return context.Canceled
		}
		return nil
	})
	if int(exitcode.FromError(err)) != 73 {
		t.Fatalf("rollback through a replaced root name must need recovery: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(moved, "projects")); statErr != nil {
		t.Fatalf("rollback acted through the replaced name: %v", statErr)
	}
}
