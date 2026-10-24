// SPDX-License-Identifier: EUPL-1.2
package workspacecfg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	baseline, err := ReadBundlePins(context.Background(), pinDirectory(t, raw))
	if err != nil || len(baseline) == 0 {
		t.Fatalf("invalid source fixture: %+v %v", baseline, err)
	}
	pin := baseline[0]
	_, digest, _ := strings.Cut(pin.Reference, "@sha256:")
	cases := map[string]string{
		"duplicate":           strings.Replace(raw, `"name": "Anchorage",`, `"name":"a","name":"b",`, 1),
		"tag_only":            strings.ReplaceAll(raw, "@sha256:"+digest, ""),
		"credential":          strings.Replace(raw, pin.Reference, "user:secret@"+pin.Reference, 1),
		"control":             strings.Replace(raw, "quay.io/bundledock/toolbox:", `quay.io/bundledock/toolbox:\u001b`, 1),
		"opaque_volume":       strings.Replace(raw, pin.Volume, "custom-tools", 1),
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
			if data == raw {
				t.Fatal("negative transformation left fixture unchanged")
			}
			if pins, err := ReadBundlePins(context.Background(), pinDirectory(t, data)); err == nil || pins != nil {
				t.Fatalf("accepted %s: %+v %v", name, pins, err)
			}
		})
	}
	for _, unsafe := range []string{"alternate_pins", "alternate_pins_symlink", "alternate_pins_directory", "file_symlink", "parent_symlink", "directory_leaf", "missing"} {
		t.Run(unsafe, func(t *testing.T) {
			dir := pinDirectory(t, raw)
			path := filepath.Join(dir, ".devcontainer/devcontainer.json")
			switch unsafe {
			case "alternate_pins":
				if err := os.WriteFile(filepath.Join(dir, ".devcontainer/bundle-pins.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "alternate_pins_symlink":
				if err := os.Symlink("missing-alternate-pins", filepath.Join(dir, ".devcontainer/bundle-pins.json")); err != nil {
					t.Fatal(err)
				}
			case "alternate_pins_directory":
				if err := os.Mkdir(filepath.Join(dir, ".devcontainer/bundle-pins.json"), 0700); err != nil {
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

func TestBundlePinsRetainsBoundRootAcrossAncestorSwap(t *testing.T) {
	ctx := context.Background()
	raw := bundleConfig(t)
	originalPins, err := ReadBundlePins(ctx, pinDirectory(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	_, digest, _ := strings.Cut(originalPins[0].Reference, "@sha256:")
	otherDigest := strings.Repeat("b", 64)
	if digest == otherDigest {
		otherDigest = strings.Repeat("c", 64)
	}
	parents := []string{t.TempDir(), t.TempDir()}
	for i, parent := range parents {
		source := filepath.Join(parent, "source")
		if err := os.MkdirAll(filepath.Join(source, ".devcontainer"), 0700); err != nil {
			t.Fatal(err)
		}
		data := raw
		if i == 1 {
			data = strings.ReplaceAll(raw, digest, otherDigest)
		}
		if err := os.WriteFile(filepath.Join(source, ".devcontainer/devcontainer.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(t.TempDir(), "current")
	if err := os.Symlink(parents[0], alias); err != nil {
		t.Fatal(err)
	}
	d, err := openDir(filepath.Join(alias, "source"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// The source descriptor is A. Reopening the same spelling now reads B.
	if err := os.Symlink(parents[1], alias+".next"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(alias+".next", alias); err != nil {
		t.Fatal(err)
	}
	pins, err := readBoundBundlePins(ctx, d, filepath.Join(parents[0], "source"))
	if err != nil || len(pins) != len(originalPins) || pins[0].Reference != originalPins[0].Reference {
		t.Fatalf("held source identity lost after ancestor swap: got=%+v err=%v want=%s", pins, err, originalPins[0].Reference)
	}
}

func TestBundlePinsRefusesReplacedLabelRootIdentity(t *testing.T) {
	source := pinDirectory(t, bundleConfig(t))
	replacement := pinDirectory(t, bundleConfig(t))
	d, err := openDir(source)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := os.Rename(source, source+".held"); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(source + ".held")
	if err := os.Rename(replacement, source); err != nil {
		t.Fatal(err)
	}
	pins, err := readBoundBundlePins(context.Background(), d, source)
	if err != ErrBundleSourceMismatch || pins != nil {
		t.Fatalf("replacement root accepted: %+v %v", pins, err)
	}
}

func TestBundlePinsRejectNoncanonicalMountKeysBeforeRelevance(t *testing.T) {
	raw := bundleConfig(t)
	digest := strings.Repeat("c", 64)
	cases := map[string]string{
		"src":                                "type=volume,src=bundledock-hidden-" + digest + ",target=/ordinary,readonly",
		"dst":                                "type=volume,source=opaque,dst=/opt/bdock/hidden,readonly",
		"destination":                        "type=volume,source=opaque,destination=/opt/bdock/hidden,readonly",
		"extra_aliased_bundle":               "type=volume,src=bundledock-hidden-" + digest + ",dst=/opt/bdock/hidden,readonly",
		"unknown_key":                        "type=volume,source=opaque,target=/ordinary,unrecognised=/opt/bdock/hidden,readonly",
		"target_double_leading_separator":    "type=volume,source=opaque,target=//opt/bdock/hidden,readonly",
		"target_double_inner_separator":      "type=volume,source=opaque,target=/opt//bdock/hidden,readonly",
		"target_dot_component":               "type=volume,source=opaque,target=/opt/./bdock/hidden,readonly",
		"target_relative":                    "type=volume,source=opaque,target=opt/bdock/hidden,readonly",
		"target_empty":                       "type=volume,source=opaque,target=,readonly",
		"target_bundle_trailing_separator":   "type=volume,source=opaque,target=/opt/bdock/hidden/,readonly",
		"target_ordinary_trailing_separator": "type=volume,source=opaque,target=/ordinary/,readonly",
	}
	for name, mount := range cases {
		t.Run(name, func(t *testing.T) {
			data := strings.Replace(raw, `"mounts": [`, `"mounts": [`+"\n"+fmt.Sprintf("%q,", mount), 1)
			if data == raw {
				t.Fatal("fixture mount insertion failed")
			}
			pins, err := ReadBundlePins(context.Background(), pinDirectory(t, data))
			if err == nil || pins != nil {
				t.Fatalf("noncanonical mount hid a declared bundle: %s pins=%+v err=%v", name, pins, err)
			}
		})
	}
}

func TestBundlePinsRejectAliasesOnOtherwiseValidMounts(t *testing.T) {
	raw := bundleConfig(t)
	baseline, err := ReadBundlePins(context.Background(), pinDirectory(t, raw))
	if err != nil || len(baseline) == 0 {
		t.Fatalf("invalid fixture: %+v %v", baseline, err)
	}
	p := baseline[0]
	canonical := "type=volume,source=" + p.Volume + ",target=" + p.Target + ",readonly,volume-nocopy"
	reordered := "ro,destination=" + p.Target + ",src=" + p.Volume + ",volume-nocopy,type=volume"
	cases := map[string]string{
		"src":                strings.ReplaceAll(raw, "source=", "src="),
		"dst":                strings.ReplaceAll(raw, "target=", "dst="),
		"destination":        strings.ReplaceAll(raw, "target=", "destination="),
		"ro":                 strings.ReplaceAll(raw, ",readonly", ",ro"),
		"combined_reordered": strings.Replace(raw, canonical, reordered, 1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if data == raw {
				t.Fatal("alias fixture transform changed no bytes")
			}
			got, err := ReadBundlePins(context.Background(), pinDirectory(t, data))
			if err == nil || got != nil {
				t.Fatalf("unsupported alias accepted on otherwise valid pins: alias=%s got=%+v err=%v", name, got, err)
			}
		})
	}
}

func TestBundlePinsRejectUnsupportedAliasesDuplicatesAndValues(t *testing.T) {
	raw := bundleConfig(t)
	pins, err := ReadBundlePins(context.Background(), pinDirectory(t, raw))
	if err != nil || len(pins) == 0 {
		t.Fatalf("invalid fixture: %+v %v", pins, err)
	}
	p := pins[0]
	canonical := "type=volume,source=" + p.Volume + ",target=" + p.Target + ",readonly,volume-nocopy"
	src := "source=" + p.Volume
	target := "target=" + p.Target
	cases := map[string]string{
		"source_then_src":         "type=volume," + src + ",src=" + p.Volume + "," + target + ",readonly",
		"src_then_source":         "type=volume,src=" + p.Volume + "," + src + "," + target + ",readonly",
		"target_then_dst":         "type=volume," + src + "," + target + ",dst=" + p.Target + ",readonly",
		"dst_then_target":         "type=volume," + src + ",dst=" + p.Target + "," + target + ",readonly",
		"target_then_destination": "type=volume," + src + "," + target + ",destination=" + p.Target + ",readonly",
		"destination_then_target": "type=volume," + src + ",destination=" + p.Target + "," + target + ",readonly",
		"dst_then_destination":    "type=volume," + src + ",dst=" + p.Target + ",destination=" + p.Target + ",readonly",
		"destination_then_dst":    "type=volume," + src + ",destination=" + p.Target + ",dst=" + p.Target + ",readonly",
		"readonly_then_ro":        "type=volume," + src + "," + target + ",readonly,ro",
		"ro_then_readonly":        "type=volume," + src + "," + target + ",ro,readonly",
		"duplicate_alias":         "type=volume," + src + "," + target + ",ro,ro",
		"ro_empty_assignment":     "type=volume," + src + "," + target + ",ro=",
		"ro_true_assignment":      "type=volume," + src + "," + target + ",ro=true",
		"ro_false_assignment":     "type=volume," + src + "," + target + ",ro=false",
		"unknown_dest":            "type=volume," + src + ",dest=" + p.Target + ",readonly",
		"unknown_readonly":        "type=volume," + src + "," + target + ",read-only",
		"uppercase_unknown":       "type=volume,SRC=" + p.Volume + "," + target + ",readonly",
	}
	for name, mount := range cases {
		t.Run(name, func(t *testing.T) {
			data := strings.Replace(raw, canonical, mount, 1)
			if data == raw {
				t.Fatal("collision fixture transform changed no bytes")
			}
			got, err := ReadBundlePins(context.Background(), pinDirectory(t, data))
			if err == nil || got != nil {
				t.Fatalf("unsupported alias, duplicate or value accepted: %s pins=%+v err=%v", name, got, err)
			}
		})
	}
}

func TestBundlePinsCanonicalMountKeyOrderPreservesPins(t *testing.T) {
	raw := bundleConfig(t)
	baseline, err := ReadBundlePins(context.Background(), pinDirectory(t, raw))
	if err != nil || len(baseline) == 0 {
		t.Fatalf("invalid canonical fixture: %+v %v", baseline, err)
	}
	p := baseline[0]
	canonical := "type=volume,source=" + p.Volume + ",target=" + p.Target + ",readonly,volume-nocopy"
	reordered := "readonly,target=" + p.Target + ",source=" + p.Volume + ",volume-nocopy,type=volume"
	data := strings.Replace(raw, canonical, reordered, 1)
	if data == raw {
		t.Fatal("canonical reorder changed no fixture bytes")
	}
	got, err := ReadBundlePins(context.Background(), pinDirectory(t, data))
	if err != nil || !reflect.DeepEqual(got, baseline) {
		t.Fatalf("canonical key order changed pins: %+v %v", got, err)
	}
}
