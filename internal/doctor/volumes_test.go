package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "diskutil", name)) //nolint:gosec // a fixture of this package
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeDiskutil answers diskutil from the fixtures, which carry made-up names.
func fakeDiskutil(t *testing.T) scripted {
	return scripted{
		"diskutil list -plist":                         fixture(t, "list.plist"),
		"diskutil info -plist /System/Volumes/Data":    fixture(t, "info_data.plist"),
		"diskutil info -plist /System/Volumes/Preboot": fixture(t, "info_preboot.plist"),
		"diskutil info -plist /nix":                    fixture(t, "info_nix.plist"),
		"diskutil info -plist /Volumes/Fake SSD":       fixture(t, "info_ssd.plist"),
		// /Volumes/Gone is listed but diskutil does not know it
	}
}

func TestVolumesAreReadFromDiskutilPlists(t *testing.T) {
	d := Deps{GOOS: "darwin", Runner: fakeDiskutil(t)}
	vols, err := d.volumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vols {
		got = append(got, v.Name+"|"+v.Mount+"|"+v.FS)
	}
	want := "Data|/System/Volumes/Data|apfs, Fake Store|/nix|apfs, Fake SSD|/Volumes/Fake SSD|exfat"
	if strings.Join(got, ", ") != want {
		t.Fatalf("volumes %q, want %q", got, want)
	}
	if !vols[0].Internal || vols[2].Internal {
		t.Error("internal flag read wrongly")
	}
	if vols[0].Free != 40000000000 || vols[2].Free != 1500000000000 {
		t.Errorf("free space %d, %d: APFS takes the container's, other types FreeSpace", vols[0].Free, vols[2].Free)
	}
	if !vols[0].APFS() || vols[2].APFS() {
		t.Error("APFS type read wrongly")
	}
}

func TestHostileOrEmptyDiskutilOutputGivesAnErrorNotAPanic(t *testing.T) {
	huge := "<plist><dict>" + strings.Repeat("<key>k</key><string>v</string>", 200000) + "</dict></plist>"
	deep := "<plist>" + strings.Repeat("<array>", 500) + strings.Repeat("</array>", 500) + "</plist>"
	many := "<plist><array>" + strings.Repeat("<true/>", 150000) + "</array></plist>"
	for name, out := range map[string]string{
		"empty":     "",
		"spaces":    "   \n",
		"not xml":   "Segmentation fault\x1b[31m",
		"truncated": "<plist><dict><key>AllDisksAndPartitions</key><array><dict>",
		"huge":      huge + strings.Repeat(" ", 5<<20),
		"too deep":  deep,
		"too many":  many,
		"bad int":   "<plist><dict><key>a</key><integer>x</integer></dict></plist>",
		"no key":    "<plist><dict><string>a</string></dict></plist>",
		"unknown":   "<plist><dict><key>a</key><blob/></dict></plist>",
		"no value":  "<plist><dict><key>a</key></dict></plist>",
		"entity":    "<!DOCTYPE plist [<!ENTITY x \"y\">]><plist><dict><key>a</key><string>&x;</string></dict></plist>",
	} {
		d := Deps{GOOS: "darwin", Runner: scripted{"diskutil list -plist": out}}
		_, err := d.volumes(context.Background())
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), "\x1b") || len(err.Error()) > 120 {
			t.Errorf("%s: raw or long error %q", name, err)
		}
	}
	if _, err := (Deps{GOOS: "darwin", Runner: scripted{}}).volumes(context.Background()); err == nil || strings.Contains(err.Error(), "exit status") && len(err.Error()) > 120 {
		t.Errorf("a failing diskutil: %v", err)
	}
	if _, err := (Deps{GOOS: "linux"}).volumes(context.Background()); err == nil {
		t.Error("volumes were listed off a Mac")
	}
	for name, info := range map[string]string{
		"read only": strings.Replace(fixture(t, "info_data.plist"), "<key>Writable</key><true/>", "<key>Writable</key><false/>", 1),
		"no name":   strings.Replace(fixture(t, "info_data.plist"), "<string>Data</string>", "<string></string>", 1),
		"no size":   strings.Replace(fixture(t, "info_data.plist"), "<integer>1000000000000</integer>", "<integer>0</integer>", 1),
		"empty":     "",
	} {
		if _, ok := parseVolume([]byte(info)); ok {
			t.Errorf("%s: accepted as a volume", name)
		}
	}
}

func TestMountPointsWithEscapesAreNotPassedToDiskutil(t *testing.T) {
	list := map[string]any{"a": []any{
		map[string]any{"MountPoint": "/Volumes/ok"},
		map[string]any{"MountPoint": "/Volumes/\x1b[2Jbad"},
		map[string]any{"MountPoint": "-rf"},
	}}
	if got := mountPoints(list); len(got) != 1 || got[0] != "/Volumes/ok" {
		t.Errorf("mount points %q", got)
	}
}

func TestSizeIsShortAndDecimal(t *testing.T) {
	for in, want := range map[int64]string{-1: "unknown", 0: "0 B", 999: "999 B", 1500000000000: "1.5 TB", 40000000000: "40.0 GB"} {
		if got := size(in); got != want {
			t.Errorf("size(%d) = %q, want %q", in, got, want)
		}
	}
}
