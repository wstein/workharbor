// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"testing"
)

func TestInitDevcontainerSelectionRetainsOriginalAncestors(t *testing.T) {
	dir := fixture(t)
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, ".devcontainer"), filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/custom.json"), []byte(`{"image":"debian:bookworm"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectDevcontainer(context.Background(), ".devcontainer/custom.json"); int(exitcode.FromError(err)) != 68 {
		t.Fatalf("changed ancestor refreshed: %v", err)
	}
}
func TestInitDevcontainerSelectionStalePreview(t *testing.T) {
	for _, name := range []string{"workharbor.json", "custom.json"} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			raw := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`)
			if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), raw, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "custom.json"), []byte(`{"image":"debian:bookworm"}`), 0644); err != nil {
				t.Fatal(err)
			}
			s, err := Discover(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			s, err = s.SelectDevcontainer(context.Background(), "custom.json")
			if err != nil {
				t.Fatal(err)
			}
			selected := "custom.json"
			p, err := Build(s, Options{Devcontainer: &selected, Overwrite: []string{"workharbor.json"}, Yes: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(`{}`), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := p.Apply(context.Background()); int(exitcode.FromError(err)) != 68 {
				t.Fatalf("stale %s accepted: %v", name, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "projects")); !os.IsNotExist(err) {
				t.Fatal("stale preview mutated tree")
			}
		})
	}
}
