// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHumanReportEscapesDirectorySubjectAndMessage(t *testing.T) {
	const hostile = "name\x1b[31m\u202e"
	const inert = "name<U+001B>[31m<U+202E>"
	for _, invalid := range []bool{false, true} {
		name := "valid"
		if invalid {
			name = "invalid"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), hostile)
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			put(t, dir, "workharbor.json", goodManifest)
			put(t, dir, ".devcontainer/devcontainer.json", goodDevc)
			put(t, dir, "projects/.gitkeep", "")
			if invalid {
				// A missing hostile path reaches both the finding subject and its message.
				manifest := map[string]any{"schema_version": 1, "devcontainer_path": "path\u202e.json", "projects_path": "projects", "bundles": []string{}}
				raw, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				put(t, dir, "workharbor.json", string(raw))
			}
			got := run(t, nil, "validate", dir)
			wantExit := 0
			if invalid {
				wantExit = 68
			}
			if got.code != wantExit {
				t.Fatalf("exit %d, want %d; stdout=%q stderr=%q", got.code, wantExit, got.stdout, got.err)
			}
			if strings.ContainsAny(got.stdout+got.err, "\x1b\u202e") {
				t.Fatalf("raw terminal controls in output: stdout=%q stderr=%q", got.stdout, got.err)
			}
			if !strings.Contains(got.stdout, "Directory: "+filepath.Join(filepath.Dir(dir), inert)+"\n") {
				t.Errorf("escaped directory absent: %q", got.stdout)
			}
			if invalid {
				want := "devcontainer.missing " + "path<U+202E>.json: path<U+202E>.json does not exist"
				if !strings.Contains(got.stdout, want) {
					t.Errorf("escaped subject and message absent: want %q in %q", want, got.stdout)
				}
			}
		})
	}
}
