// SPDX-License-Identifier: EUPL-1.2
package workspacecfg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bundleConfig(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/bundle-drift/anchorage-devcontainer.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func pinDirectory(t *testing.T, raw string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestBundlePinsActualAnchorageConfiguration(t *testing.T) {
	raw := bundleConfig(t)
	pins, err := ReadBundlePins(context.Background(), pinDirectory(t, raw))
	if err != nil || len(pins) != 6 {
		t.Fatalf("pins=%+v err=%v", pins, err)
	}
	for _, p := range pins {
		if p.Volume != "bundledock-"+p.Bundle+"-"+strings.Split(p.Reference, "@sha256:")[1] || p.Target != "/opt/bdock/"+p.Bundle {
			t.Fatalf("%+v", p)
		}
	}
	// The same source is valid JSONC. Host-info remains an inert non-bundle.
	_, err = ReadBundlePins(context.Background(), pinDirectory(t, strings.Replace(raw, "{", "{ // comment", 1)))
	if err != nil {
		t.Fatal(err)
	}
}
func TestBundlePinsRejectAmbiguousUnsafeAndOpaqueConfiguration(t *testing.T) {
	raw := bundleConfig(t)
	cases := map[string]string{
		"duplicate":           strings.Replace(raw, `"name": "Anchorage",`, `"name":"a","name":"b",`, 1),
		"tag_only":            strings.ReplaceAll(raw, "@sha256:c84e2e79315393b62a156cc3529a29229da71d9c44b1341be0a3e6dfc1d1afb6", ""),
		"credential":          strings.Replace(raw, "quay.io/bundledock/toolbox:", "user:secret@quay.io/bundledock/toolbox:", 1),
		"control":             strings.Replace(raw, "quay.io/bundledock/toolbox:", `quay.io/bundledock/toolbox:\u001b`, 1),
		"opaque_volume":       strings.Replace(raw, "bundledock-toolbox-c84e2e79315393b62a156cc3529a29229da71d9c44b1341be0a3e6dfc1d1afb6", "custom-tools", 1),
		"unknown_hook":        strings.Replace(raw, "initialize.sh", "other.sh", 1),
		"unknown_branch":      strings.Replace(raw, `"toolbox": [`, `"other": [`, 1),
		"unknown_host":        strings.Replace(raw, "host-info.sh", "anything.sh", 1),
		"readonly_equals":     strings.Replace(raw, "readonly,volume-nocopy", "readonly=,volume-nocopy", 1),
		"nocopy_equals":       strings.Replace(raw, "readonly,volume-nocopy", "readonly,volume-nocopy=", 1),
		"writable":            strings.Replace(raw, "readonly,volume-nocopy", "volume-nocopy", 1),
		"duplicate_mount_key": strings.Replace(raw, "type=volume,source=", "type=volume,type=volume,source=", 1),
		"missing_mount":       strings.Replace(raw, "target=/opt/bdock/toolbox", "target=/elsewhere", 1),
		"large":               strings.Repeat(" ", 64<<10) + raw,
		"nul":                 raw + "\x00", "bom": "\xef\xbb\xbf" + raw, "invalid_utf8": raw + "\xff",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if pins, err := ReadBundlePins(context.Background(), pinDirectory(t, data)); err == nil || pins != nil {
				t.Fatalf("accepted %s: %+v %v", name, pins, err)
			}
		})
	}
	for _, unsafe := range []string{"alternate_pins", "file_symlink", "parent_symlink", "directory_leaf", "missing"} {
		t.Run(unsafe, func(t *testing.T) {
			dir := pinDirectory(t, raw)
			path := filepath.Join(dir, ".devcontainer/devcontainer.json")
			switch unsafe {
			case "alternate_pins":
				if err := os.WriteFile(filepath.Join(dir, ".devcontainer/bundle-pins.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "file_symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			case "parent_symlink":
				if err := os.Rename(filepath.Join(dir, ".devcontainer"), filepath.Join(dir, "original")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("original", filepath.Join(dir, ".devcontainer")); err != nil {
					t.Fatal(err)
				}
			case "directory_leaf":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ReadBundlePins(context.Background(), dir); err == nil {
				t.Fatal("unsafe pins accepted")
			}
		})
	}
}
func TestBundlePinsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadBundlePins(ctx, "missing"); err != context.Canceled {
		t.Fatal(err)
	}
}
