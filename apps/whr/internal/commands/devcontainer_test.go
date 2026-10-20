// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func customDevcontainer(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "custom config"), 0755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("{ // preserve all custom startup and PATH settings\n \"image\": \"debian:bookworm\", \"remoteEnv\": {\"PATH\": \"/mine:${containerEnv:PATH}\"}, \"postStartCommand\": \"touch SHOULD_NOT_EXIST\", }\n")
	if err := os.WriteFile(filepath.Join(dir, "custom config/dev.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, raw
}
func TestInitDevcontainerNeverModified(t *testing.T) {
	dir, raw := customDevcontainer(t)
	name := filepath.Join(dir, "custom config/dev.json")
	before, _ := os.Stat(name)
	for _, mode := range []string{"dry", "write", "rerun"} {
		args := []string{"init", dir, "--devcontainer=custom config/dev.json", "--json", "--non-interactive", "--yes"}
		if mode == "dry" {
			args = append(args, "--dry-run")
		}
		got := run(t, nil, args...)
		if got.code != 0 {
			t.Fatalf("%s: %+v", mode, got)
		}
		if !strings.Contains(got.err, "reuse file custom config/dev.json") || !strings.Contains(got.err, "not creation-ready") {
			t.Fatalf("missing preview/readiness: %s", got.err)
		}
		env := decodeEnvelope(t, got.stdout)
		result := env["result"].(map[string]any)
		for _, v := range result["actions"].([]any) {
			a := v.(map[string]any)
			if (mode == "dry" || mode == "rerun") && a["applied"] != false {
				t.Fatalf("%s mutated %v", mode, a)
			}
		}
		if mode == "dry" {
			if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
				t.Fatal("dry run wrote manifest")
			}
		}
	}
	after, _ := os.Stat(name)
	actual, _ := os.ReadFile(name)
	if !bytes.Equal(actual, raw) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		t.Fatal("existing devcontainer changed")
	}
	var m map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["devcontainer_path"] != "custom config/dev.json" {
		t.Fatal("manifest did not select file")
	}
	if _, err := os.Stat(filepath.Join(dir, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatal("startup executed")
	}
}
func TestInitDevcontainerPathTraversalRefused(t *testing.T) {
	for _, path := range []string{"../out.json", "a/../out.json", "/tmp/out.json", "./custom.json", "a//b.json", "", "a\\b.json", ".git/config"} {
		dir, _ := customDevcontainer(t)
		got := run(t, nil, "init", dir, "--dry-run", "--json", "--devcontainer="+path)
		if got.code != 64 {
			t.Fatalf("%q: %+v", path, got)
		}
		if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
			t.Fatal("refusal wrote manifest")
		}
	}
}
func TestInitDevcontainerSymlinkRefused(t *testing.T) {
	for _, ancestor := range []bool{false, true} {
		dir, _ := customDevcontainer(t)
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "dev.json"), []byte(`{"image":"debian:bookworm"}`), 0644); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(outside, "dev.json")
		selected := "link"
		if ancestor {
			target = outside
			selected = "link/dev.json"
		}
		if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
		got := run(t, nil, "init", dir, "--dry-run", "--json", "--devcontainer="+selected)
		if got.code != 68 {
			t.Fatalf("symlink accepted: %+v", got)
		}
	}
}
func TestInitDevcontainerMissingAndInvalidJSONC(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		code int
	}{{"", 70}, {`{"image":`, 68}, {`{"image":"a","image":"b"}`, 68}} {
		dir := t.TempDir()
		if tc.raw != "" {
			if err := os.WriteFile(filepath.Join(dir, "dev.json"), []byte(tc.raw), 0644); err != nil {
				t.Fatal(err)
			}
		}
		got := run(t, nil, "init", dir, "--dry-run", "--json", "--devcontainer=dev.json")
		if got.code != tc.code {
			t.Fatalf("%q: %+v", tc.raw, got)
		}
	}
}
