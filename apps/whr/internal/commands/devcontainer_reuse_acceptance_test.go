// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the public CLI writer as well as proposal construction: reuse must
// preserve startup files even when changing an existing manifest's selection.
func TestInitDevcontainerReusePreservesAppliedStartupFiles(t *testing.T) {
	for _, existingManifest := range []bool{false, true} {
		name := "new_manifest"
		if existingManifest {
			name = "replace_manifest"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "HOOK_EXECUTED")
			hook, err := json.Marshal("touch '" + marker + "'")
			if err != nil {
				t.Fatal(err)
			}
			selected := "custom config/devcontainer.json"
			custom := []byte("{ // retain JSONC and custom PATH verbatim\n \"image\": \"debian:bookworm\", \"remoteEnv\": {\"PATH\": \"/mine:${containerEnv:PATH}\"}, \"postStartCommand\": " + string(hook) + ",\n}\n")
			originalManifest := []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}` + "\n")
			files := map[string][]byte{
				selected:                          custom,
				".devcontainer/devcontainer.json": []byte("{ // original selection also stays untouched\n \"image\":\"debian:bookworm\"\n}\n"),
				".bashrc":                         []byte("# preserve bash\nexport PATH=\"/bash custom:$PATH\"\n"),
				".profile":                        []byte("export GOBIN=\"$HOME/my bin\"\n"),
				".zshrc":                          []byte("export PATH=\"/zsh custom:$PATH\"\n"),
				".config/fish/config.fish":        []byte("set -gx PATH '/fish custom' $PATH\n"),
			}
			type observation struct {
				raw  []byte
				info os.FileInfo
			}
			before := make(map[string]observation)
			stamp := time.Unix(1700000000, 0)
			for relative, raw := range files {
				target := filepath.Join(dir, relative)
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(target, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				before[relative] = observation{raw: raw, info: info}
			}
			if existingManifest {
				if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), originalManifest, 0644); err != nil {
					t.Fatal(err)
				}
			}
			for _, mode := range []string{"dry", "write", "rerun"} {
				args := []string{"init", dir, "--devcontainer=" + selected, "--non-interactive", "--yes", "--json"}
				if existingManifest {
					args = append(args, "--overwrite=workharbor.json")
				}
				if mode == "dry" {
					args = append(args, "--dry-run")
				}
				got := run(t, nil, args...)
				if got.code != 0 {
					t.Fatalf("%s: %+v", mode, got)
				}
				if !strings.Contains(got.err, "reuse file "+selected) {
					t.Fatalf("%s: missing reuse preview: %s", mode, got.err)
				}
				for relative, saved := range before {
					target := filepath.Join(dir, relative)
					raw, err := os.ReadFile(target)
					if err != nil {
						t.Fatalf("%s %s: %v", mode, relative, err)
					}
					info, err := os.Lstat(target)
					if err != nil {
						t.Fatalf("%s %s: %v", mode, relative, err)
					}
					if !bytes.Equal(raw, saved.raw) || info.Mode() != saved.info.Mode() || !os.SameFile(info, saved.info) || !info.ModTime().Equal(saved.info.ModTime()) {
						t.Fatalf("%s changed preserved file %s (bytes, mode, identity or mtime)", mode, relative)
					}
				}
				if _, err := os.Lstat(marker); !os.IsNotExist(err) {
					t.Fatalf("%s: startup hook executed or marker check failed: %v", mode, err)
				}
				manifestRaw, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
				if mode == "dry" {
					if existingManifest {
						if err != nil || !bytes.Equal(manifestRaw, originalManifest) {
							t.Fatalf("dry run changed existing manifest: %v", err)
						}
					} else if !os.IsNotExist(err) {
						t.Fatalf("dry run created manifest: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					var manifest map[string]any
					if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
						t.Fatal(err)
					}
					if manifest["devcontainer_path"] != selected {
						t.Fatalf("%s: wrong manifest selection: %v", mode, manifest)
					}
				}
				result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
				for _, value := range result["actions"].([]any) {
					action := value.(map[string]any)
					if (mode == "dry" || mode == "rerun") && action["applied"] != false {
						t.Fatalf("%s applied %v", mode, action)
					}
					if action["path"] == selected && (action["action"] != "reuse" || action["applied"] != false) {
						t.Fatalf("%s did not reuse selected file: %v", mode, action)
					}
				}
			}
		})
	}
}
