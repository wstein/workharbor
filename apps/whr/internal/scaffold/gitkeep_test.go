// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInitGitkeepUnderProjectsPath(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "create_directories"
		if existing {
			name = "reuse_directories"
		}
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			const projects = "source/components"
			const placeholder = projects + "/.gitkeep"
			manifest := []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"" + projects + "\",\"bundles\":[]}\n")
			manifestPath := filepath.Join(dir, "workharbor.json")
			if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
				t.Fatal(err)
			}
			projectsPath := filepath.Join(dir, filepath.FromSlash(projects))
			var originalDirectory os.FileInfo
			if existing {
				if err := os.MkdirAll(projectsPath, 0755); err != nil {
					t.Fatal(err)
				}
				var err error
				originalDirectory, err = os.Stat(projectsPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			options := Options{NonInteractive: true, Yes: true}
			proposal := applyProposal(t, dir, options)
			data, found := proposal.Files()[placeholder]
			if !found || len(data) != 0 {
				t.Fatalf("proposal must contain an empty %s, found=%t data=%q", placeholder, found, data)
			}
			if _, found := proposal.Files()["projects/.gitkeep"]; found {
				t.Fatal("proposal used the default projects path instead of the manifest path")
			}
			matched := 0
			for _, action := range proposal.Preview().Actions {
				if action.Path == placeholder {
					matched++
					if action.Kind != "file" || action.Action != "create" || action.Applied {
						t.Fatalf("unexpected placeholder preview: %+v", action)
					}
				}
			}
			if matched != 1 {
				t.Fatalf("placeholder preview count=%d, want 1", matched)
			}
			result, err := proposal.Apply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.Validation == nil || !result.Validation.Valid || result.GitChecks != "not_run" {
				t.Fatal("written custom-path proposal did not pass Git-free validation")
			}
			placeholderPath := filepath.Join(dir, filepath.FromSlash(placeholder))
			written, err := os.ReadFile(placeholderPath)
			if err != nil || len(written) != 0 {
				t.Fatalf("custom placeholder must be an empty file: %q, %v", written, err)
			}
			if _, err := os.Lstat(filepath.Join(dir, "projects")); !os.IsNotExist(err) {
				t.Fatalf("init must not create the default projects directory: %v", err)
			}
			retained, err := os.ReadFile(manifestPath)
			if err != nil || !bytes.Equal(retained, manifest) {
				t.Fatalf("init rewrote the existing manifest: %q, %v", retained, err)
			}
			if existing {
				current, err := os.Stat(projectsPath)
				if err != nil || !os.SameFile(originalDirectory, current) {
					t.Fatalf("init replaced the existing projects directory: %v", err)
				}
			}
			before, err := os.Stat(placeholderPath)
			if err != nil {
				t.Fatal(err)
			}
			result, err = applyProposal(t, dir, options).Apply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range result.Actions {
				if action.Applied {
					t.Fatalf("custom-path rerun changed %s", action.Path)
				}
			}
			after, err := os.Stat(placeholderPath)
			if err != nil || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
				t.Fatalf("no-op rerun changed the custom placeholder: %v", err)
			}
		})
	}
}
