package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/render"
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

func chooser(t *testing.T) Deps {
	return Deps{GOOS: "darwin", Runner: fakeDiskutil(t), Home: "/Users/fake"}
}

func TestTheWorkspaceVolumeIsChosenByNumberWithADefault(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		lines []string
		want  string
	}{
		"Enter is the data volume": {[]string{""}, "/Users/fake/workspaces"},
		"2 is the store":           {[]string{"2"}, "/nix/workspaces"},
		"other path":               {[]string{"4", "/Volumes/else/ws/../ws"}, "/Volumes/else/ws"},
		"other path default":       {[]string{"4", ""}, "/Users/fake/workspaces"},
	} {
		a := &answers{lines: tc.lines}
		got, err := chooser(t).chooseWorkspaces(ctx, a)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, tc.want)
		}
	}
}

func TestChoosingNothingChoosesNothing(t *testing.T) {
	ctx := context.Background()
	for name, lines := range map[string][]string{
		"q":              {"q"},
		"quit":           {"QUIT"},
		"q for the path": {"4", "q"},
		"zero":           {"0"},
		"too big":        {"5"},
		"text":           {"ssd"},
		"relative path":  {"4", "ws"},
		"escape path":    {"4", "/tmp/\u009b2J"},
	} {
		got, err := chooser(t).chooseWorkspaces(ctx, &answers{lines: lines})
		if err == nil || got != "" {
			t.Errorf("%s: %q, %v", name, got, err)
		}
		if strings.Contains(name, "q") && name != "too big" && !errors.Is(err, render.ErrQuit) && name != "zero" {
			t.Errorf("%s: %v is not a quit", name, err)
		}
	}
}

func TestTheChoiceFitsEightyColumnsAndEscapesDiskNames(t *testing.T) {
	r := fakeDiskutil(t)
	r["diskutil info -plist /Volumes/Fake SSD"] = strings.Replace(fixture(t, "info_ssd.plist"),
		"<string>Fake SSD</string>", "<string>&#x9b;2J&#x202e;"+strings.Repeat("long", 40)+"</string>", 1)
	d := Deps{GOOS: "darwin", Runner: r, Home: "/Users/fake"}
	a := &answers{lines: []string{""}}
	if _, err := d.chooseWorkspaces(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	shown := strings.Join(a.shown, "\n")
	for _, l := range strings.Split(shown, "\n") {
		if utf8.RuneCountInString(l) > 80 {
			t.Errorf("%d columns: %q", utf8.RuneCountInString(l), l)
		}
	}
	for _, bad := range []string{"\u009b", "\u202e"} {
		if strings.Contains(shown, bad) {
			t.Errorf("the list shows an unescaped %q", bad)
		}
	}
	for _, want := range []string{`\u009b2J`, "APFS", "internal", "external", "other (exfat)", "40.0 GB free", "1.0 TB", "4) other path"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the list lacks %q:\n%s", want, shown)
		}
	}
}

func TestYesTakesTheDefaultVolumeAndAsksNothing(t *testing.T) {
	d := chooser(t)
	d.Yes = true
	got, err := d.chooseWorkspaces(context.Background(), &answers{})
	if err != nil || got != "/Users/fake/workspaces" {
		t.Errorf("%q, %v", got, err)
	}
}

func TestWithoutADiskListOnlyAPathIsAsked(t *testing.T) {
	d := Deps{GOOS: "darwin", Runner: scripted{"diskutil list -plist": "\x1b[31mgarbage"}, Home: "/Users/fake"}
	a := &answers{lines: []string{""}}
	got, err := d.chooseWorkspaces(context.Background(), a)
	if err != nil || got != "/Users/fake/workspaces" {
		t.Errorf("%q, %v", got, err)
	}
	if s := strings.Join(a.shown, "\n"); strings.Contains(s, "\x1b") || !strings.Contains(s, "not available") {
		t.Errorf("shown %q", s)
	}
}

func TestWithoutAReadableDataVolumeNoVolumeIsGuessed(t *testing.T) {
	r := fakeDiskutil(t)
	delete(r, "diskutil info -plist /System/Volumes/Data")
	d := Deps{GOOS: "darwin", Runner: r, Home: "/Users/fake"}
	a := &answers{lines: []string{"", ""}}
	got, err := d.chooseWorkspaces(context.Background(), a)
	if err != nil || got != "/Users/fake/workspaces" {
		t.Errorf("Enter: %q, %v", got, err)
	}
	// Enter chose "other path" (a second question was asked: two lines were used)
	if len(a.lines) != 0 {
		t.Errorf("%d lines left", len(a.lines))
	}
	d.Yes = true
	b := &answers{}
	if got, err := d.chooseWorkspaces(context.Background(), b); err != nil || got != "/Users/fake/workspaces" || !strings.Contains(strings.Join(b.shown, "\n"), "yes: volume 3") {
		t.Errorf("--yes: %q, %v", got, err)
	}
}

func TestTheDataVolumeIsTheDefaultWhereverItIs(t *testing.T) {
	r := fakeDiskutil(t)
	r["diskutil list -plist"] = "<plist><array><dict><key>MountPoint</key><string>/nix</string></dict><dict><key>MountPoint</key><string>/System/Volumes/Data</string></dict></array></plist>"
	d := Deps{GOOS: "darwin", Runner: r, Home: "/Users/fake"}
	d.Yes = true
	a := &answers{}
	got, err := d.chooseWorkspaces(context.Background(), a)
	if err != nil || got != "/Users/fake/workspaces" || !strings.Contains(strings.Join(a.shown, "\n"), "yes: volume 2") {
		t.Errorf("%q, %v, %v", got, err, a.shown)
	}
}

func TestOnlyMaxVolumesMountPointsAreAsked(t *testing.T) {
	var entries []any
	for i := range maxVolumes + 10 {
		entries = append(entries, map[string]any{"MountPoint": "/Volumes/v" + strings.Repeat("x", i)})
	}
	if got := mountPoints(entries); len(got) != maxVolumes {
		t.Errorf("%d mount points", len(got))
	}
}

func TestADiskListErrorIsEscapedAndShort(t *testing.T) {
	d := Deps{GOOS: "darwin", Runner: scripted{"diskutil list -plist": "ERR:\u009b2J" + strings.Repeat("x", 500)}, Home: "/h"}
	a := &answers{lines: []string{""}}
	if _, err := d.chooseWorkspaces(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if s := a.shown[0]; strings.Contains(s, "\u009b") || len(s) > 160 {
		t.Errorf("%q", s)
	}
}

func TestPlistLimitsFailWithTheirOwnError(t *testing.T) {
	nestedDicts := "<plist>" + strings.Repeat("<dict><key>k</key>", 30) + "<true/>" + strings.Repeat("</dict>", 30) + "</plist>"
	for name, tc := range map[string]struct{ in, want string }{
		"26 arrays":    {"<plist>" + strings.Repeat("<array>", 26) + strings.Repeat("</array>", 26) + "</plist>", "nested too deeply"},
		"30 dicts":     {nestedDicts, "nested too deeply"},
		"150k nodes":   {"<plist><array>" + strings.Repeat("<true/>", 150000) + "</array></plist>", "too many entries"},
		"5 MiB string": {"<plist><string>" + strings.Repeat("a", 5<<20) + "</string></plist>", "too large"},
		"external DTD": {`<!DOCTYPE plist SYSTEM "http://example.invalid/x.dtd"><plist><string>&ext;</string></plist>`, "not valid XML"},
		"mismatch":     {"<plist><dict></array></plist>", "not valid XML"},
		"20-digit int": {"<plist><integer>" + strings.Repeat("9", 20) + "</integer></plist>", "not a number"},
	} {
		if _, err := parsePlist([]byte(tc.in)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	if _, err := parsePlist([]byte("<plist>" + strings.Repeat("<array>", 24) + strings.Repeat("</array>", 24) + "</plist>")); err != nil {
		t.Errorf("24 nested arrays are within the limit: %v", err)
	}
}

// apfsSSD is the fixture's external disk as an APFS volume with the keys read
// on the boot volume: FileVault and GlobalPermissionsEnabled.
func apfsSSD(t *testing.T, extra string) string {
	t.Helper()
	return strings.Replace(strings.Replace(fixture(t, "info_ssd.plist"), "exfat", "apfs", 1),
		"<key>Internal</key>", extra+"<key>Internal</key>", 1)
}

func TestOnlyAPFSVolumesCanBeChosenNotEvenWithYes(t *testing.T) {
	ctx := context.Background()
	d := chooser(t) // volume 3 is exFAT
	a := &answers{lines: []string{"3"}}
	got, err := d.chooseWorkspaces(ctx, a)
	if err == nil || got != "" || !strings.Contains(err.Error(), "not usable") {
		t.Errorf("exFAT chosen: %q, %v", got, err)
	}
	if !strings.Contains(strings.Join(a.shown, "\n"), "not usable: no Unix owners or modes") {
		t.Errorf("the list does not say why:\n%v", a.shown)
	}
	// --yes takes the default; an unusable default is still refused
	d.Yes = true
	if _, err := d.pick(&answers{}, Volume{Name: "x", Mount: "/Volumes/x", FS: "msdos"}); err == nil {
		t.Error("--yes picked a FAT volume")
	}
	for _, fs := range []string{"exfat", "msdos", "ntfs", "hfs"} {
		if ok, _ := (Volume{FS: fs}).Usable(); ok {
			t.Errorf("%s is usable", fs)
		}
	}
}

func TestEncryptionAndOwnershipAreReadFromVerifiedKeysOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		extra      string
		enc, owner Tri
		warns      int
	}{
		"unencrypted, ownership ignored": {"<key>FileVault</key><false/><key>GlobalPermissionsEnabled</key><false/>", No, No, 2},
		"encrypted, honoured":            {"<key>FileVault</key><true/><key>GlobalPermissionsEnabled</key><true/>", Yes, Yes, 0},
		"keys missing":                   {"", Unknown, Unknown, 0},
		"not booleans":                   {"<key>FileVault</key><string>Yes</string>", Unknown, Unknown, 0},
		// Encryption is true on the boot volume but is not read: unverified outside it
		"only Encryption": {"<key>Encryption</key><false/>", Unknown, Unknown, 0},
	} {
		v, ok := parseVolume([]byte(apfsSSD(t, tc.extra)))
		if !ok || v.Encrypted != tc.enc || v.Owners != tc.owner || len(v.Warnings()) != tc.warns {
			t.Errorf("%s: %+v warnings %v", name, v, v.Warnings())
		}
	}
}

func TestAnUnencryptedExternalAPFSVolumeWarnsAndIsStillChosen(t *testing.T) {
	r := fakeDiskutil(t)
	r["diskutil info -plist /Volumes/Fake SSD"] = apfsSSD(t, "<key>FileVault</key><false/><key>GlobalPermissionsEnabled</key><false/>")
	d := Deps{GOOS: "darwin", Runner: r, Home: "/Users/fake"}
	a := &answers{lines: []string{"3"}}
	got, err := d.chooseWorkspaces(context.Background(), a)
	if err != nil || got != "/Volumes/Fake SSD/workspaces" {
		t.Fatalf("%q, %v", got, err)
	}
	shown := strings.Join(a.shown, "\n")
	for _, want := range []string{"encryption: no", "not encrypted", "ownership is ignored", "FileVault and restarts"} {
		if !strings.Contains(shown, want) {
			t.Errorf("missing %q:\n%s", want, shown)
		}
	}
	for _, l := range strings.Split(shown, "\n") {
		if utf8.RuneCountInString(l) > 80 {
			t.Errorf("%d columns: %q", utf8.RuneCountInString(l), l)
		}
	}
	// an unreadable state is shown as unknown and is not a warning
	r["diskutil info -plist /Volumes/Fake SSD"] = apfsSSD(t, "")
	a = &answers{lines: []string{"3"}}
	if _, err := d.chooseWorkspaces(context.Background(), a); err != nil || !strings.Contains(strings.Join(a.shown, "\n"), "encryption: unknown") || strings.Contains(strings.Join(a.shown, "\n"), "warning") {
		t.Errorf("%v %v", err, a.shown)
	}
}
