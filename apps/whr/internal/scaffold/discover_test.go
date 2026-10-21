// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte("{\n // PATH stays untouched\n \"image\":\"debian:bookworm\",\"remoteEnv\":{\"PATH\":\"/custom/bin:${containerEnv:PATH}\"}\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestInitExistingDevcontainerPathPreserved(t *testing.T) {
	dir := fixture(t)
	name := ".devcontainer/devcontainer.json"
	before, _ := os.ReadFile(filepath.Join(dir, name))
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Files()[name]) != string(before) {
		t.Fatal("devcontainer JSONC/PATH was changed")
	}
}
func TestInitDryRunWritesAndRunsNothing(t *testing.T) {
	dir := fixture(t)
	before, _ := os.Stat(dir)
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(s, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.HumanPreview()
	for _, name := range []string{"workharbor.json", "projects", "AGENTS.md", ".claude"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("dry proposal created %s", name)
		}
	}
	after, _ := os.Stat(dir)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("discovery changed directory metadata")
	}
}
func TestInitExistingShellSettingsPreserved(t *testing.T) {
	dir := fixture(t)
	rc := []byte("export PATH=/custom/bin:$PATH\n")
	if err := os.WriteFile(filepath.Join(dir, ".bashrc"), rc, 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Entries[".bashrc"]; ok {
		t.Fatal("unrelated startup file was read")
	}
	if _, err := Build(s, Options{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".bashrc"))
	if string(after) != string(rc) {
		t.Fatal("startup settings changed")
	}
}
func TestInitDiscoveryRefusesUnsafeTargets(t *testing.T) {
	for _, name := range []string{"workharbor.json", ".devcontainer", ".devcontainer/devcontainer.json", "projects"} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			target := filepath.Join(dir, name)
			if err := os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), target); err != nil {
				t.Fatal(err)
			}
			_, err := Discover(context.Background(), dir)
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("symlink was not refused: %v", err)
			}
		})
	}
	dir := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, ".whr-init-tmp-orphan"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Discover(context.Background(), dir)
	if int(exitcode.FromError(err)) != 68 {
		t.Fatal("leftover temporary file was not refused")
	}
}
func TestInitDiscoveryCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Discover(ctx, "must-not-be-opened")
	if int(exitcode.FromError(err)) != 72 {
		t.Fatalf("cancelled discovery: %v", err)
	}
}
func TestInitDiscoveryRaceRefused(t *testing.T) {
	for _, raceAt := range []string{".devcontainer", ".devcontainer/devcontainer.json"} {
		t.Run(raceAt, func(t *testing.T) {
			dir := fixture(t)
			once := false
			_, err := discover(context.Background(), dir, func(name string) {
				if name != raceAt || once {
					return
				}
				once = true
				if e := os.Rename(filepath.Join(dir, ".devcontainer"), filepath.Join(dir, "moved")); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Join(dir, "moved"), filepath.Join(dir, ".devcontainer")); e != nil {
					t.Fatal(e)
				}
			})
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("raced ancestor was not refused: %v", err)
			}
		})
	}
}

func TestInitDiscoveryRootSuffixCannotHideSymlink(t *testing.T) {
	real := fixture(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/", "/.", "//././"} {
		t.Run("link"+suffix, func(t *testing.T) {
			_, err := Discover(context.Background(), link+suffix)
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("symlink root suffix %q accepted: %v", suffix, err)
			}
		})
		t.Run("real"+suffix, func(t *testing.T) {
			if _, err := Discover(context.Background(), real+suffix); err != nil {
				t.Fatalf("real directory suffix %q refused: %v", suffix, err)
			}
		})
	}
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(t.TempDir(), "ancestor")
	if err := os.Symlink(parent, ancestor); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(context.Background(), ancestor+"/child/."); err != nil {
		t.Fatalf("permitted ancestor link refused: %v", err)
	}
	if _, err := Discover(context.Background(), real+"/absent/../."); err == nil {
		t.Fatal("lexical dot-dot cleanup bypassed missing component")
	}
}

func TestInitFingerprintRetainsObservedIdentity(t *testing.T) {
	dir := fixture(t)
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
	fp := p.Fingerprints()
	for _, name := range []string{".", ".devcontainer", ".devcontainer/devcontainer.json"} {
		got, ok := fp[name]
		if !ok || !got.Exists || got.Info == nil {
			t.Fatalf("%s: observed entry lacks retained identity: %+v", name, got)
		}
		disk, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(got.Info, disk) || got.Mode != disk.Mode() {
			t.Fatalf("%s: retained identity does not name the observed object", name)
		}
	}
	for _, name := range []string{"workharbor.json", "projects"} {
		if got := fp[name]; got.Exists || got.Info != nil {
			t.Fatalf("%s: absent target must carry no identity: %+v", name, got)
		}
	}
}
