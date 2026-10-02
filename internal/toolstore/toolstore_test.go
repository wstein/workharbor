package toolstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var bg = context.Background()

// newStore returns a store whose read-only entries are made removable again
// before the temporary directory is cleaned up.
func newStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() { makeWritable(root) })
	return &Store{Root: root, allowHTTP: true} // httptest serves plain http
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// vendor is a fake release host: <base>/<version>/<platform>/<name> and the
// manifest with the SHA-256 it claims.
type vendor struct {
	srv      *httptest.Server
	binary   []byte
	claimed  string // what the manifest says; defaults to the binary's hash
	hits     int
	manifest int
}

func newVendor(t *testing.T, binary []byte) *vendor {
	t.Helper()
	v := &vendor{binary: binary, claimed: sum(binary)}
	mux := http.NewServeMux()
	mux.HandleFunc("/rel/1.2.3/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		v.manifest++
		fmt.Fprintf(w, `{"version":"1.2.3","platforms":{"linux-arm64":{"binary":"tool","checksum":%q}}}`, v.claimed)
	})
	mux.HandleFunc("/rel/1.2.3/linux-arm64/tool", func(w http.ResponseWriter, _ *http.Request) {
		v.hits++
		_, _ = w.Write(v.binary)
	})
	v.srv = httptest.NewServer(mux)
	t.Cleanup(v.srv.Close)
	return v
}

func (v *vendor) pin(hash string) Pin {
	return Pin{Name: "tool", Version: "1.2.3", Platform: "linux-arm64", BaseURL: v.srv.URL + "/rel", SHA256: hash}
}

func TestPinnedDownloadIsVerifiedAndStoredReadOnly(t *testing.T) {
	bin := []byte("#!/bin/sh\necho tool\n")
	v := newVendor(t, bin)
	s := newStore(t)
	e, err := s.Download(bg, v.pin(sum(bin)))
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(s.Root, "store", sum(bin)[:8]+"-tool-1.2.3-linux-arm64")
	if e.Dir != wantDir || e.Path() != filepath.Join(wantDir, "bin", "tool") {
		t.Errorf("entry = %+v, want %s", e, wantDir)
	}
	got, err := os.ReadFile(e.Path())
	if err != nil || string(got) != string(bin) {
		t.Fatalf("the stored tool differs: %q, %v", got, err)
	}
	// Read-only: the tool, its bin directory and the entry.
	for _, p := range []string{e.Path(), filepath.Dir(e.Path()), e.Dir} {
		info, _ := os.Stat(p)
		if info.Mode().Perm()&0o222 != 0 {
			t.Errorf("%s is writable (%v)", p, info.Mode())
		}
	}
	if err := os.WriteFile(e.Path(), []byte("x"), 0o600); err == nil {
		t.Error("the stored tool could be overwritten")
	}
	if p := s.Verify(); len(p) != 0 {
		t.Errorf("a fresh store does not verify: %v", p)
	}
	if rec, err := os.ReadFile(filepath.Join(e.Dir, RecordedHashFile)); err != nil || strings.TrimSpace(string(rec)) != e.SHA256 { //nolint:gosec // a test file
		t.Errorf("the full hash was not recorded in the entry: %q, %v", rec, err)
	}
	// Adding it again is a no-op.
	again, err := s.Download(bg, v.pin(sum(bin)))
	if err != nil || again.Dir != e.Dir {
		t.Errorf("a second download: %+v, %v", again, err)
	}
	// No staging directory is left.
	entries, _ := os.ReadDir(filepath.Join(s.Root, "store"))
	for _, d := range entries {
		if strings.HasPrefix(d.Name(), ".") {
			t.Errorf("leftover %s", d.Name())
		}
	}
}

func TestAChecksumMismatchIsRefusedAndStoresNothing(t *testing.T) {
	bin := []byte("the real tool")
	t.Run("the download is not what is pinned", func(t *testing.T) {
		v := newVendor(t, []byte("a tampered tool"))
		v.claimed = sum(bin) // the manifest agrees with the pin, the file does not
		s := newStore(t)
		if _, err := s.Download(bg, v.pin(sum(bin))); !errors.Is(err, ErrChecksum) {
			t.Fatalf("Download = %v, want ErrChecksum", err)
		}
		assertEmpty(t, s)
	})
	t.Run("the vendor manifest disagrees with the pin", func(t *testing.T) {
		v := newVendor(t, bin)
		v.claimed = sum([]byte("something else"))
		s := newStore(t)
		if _, err := s.Download(bg, v.pin(sum(bin))); !errors.Is(err, ErrChecksum) {
			t.Fatalf("Download = %v, want ErrChecksum", err)
		}
		if v.hits != 0 {
			t.Error("the binary was downloaded although the manifest already disagreed")
		}
		assertEmpty(t, s)
	})
	t.Run("a pin without a hash", func(t *testing.T) {
		v := newVendor(t, bin)
		s := newStore(t)
		if _, err := s.Download(bg, v.pin("")); !errors.Is(err, ErrChecksum) {
			t.Errorf("an unpinned download = %v, want ErrChecksum", err)
		}
		if _, err := s.Download(bg, v.pin("ABCDEF")); !errors.Is(err, ErrChecksum) {
			t.Errorf("a malformed pin = %v", err)
		}
	})
	t.Run("a vendor that is down", func(t *testing.T) {
		s := newStore(t)
		p := Pin{Name: "tool", Version: "1.2.3", Platform: "linux-arm64", BaseURL: "http://127.0.0.1:1/rel", SHA256: sum(bin)}
		if _, err := s.Download(bg, p); err == nil {
			t.Error("an unreachable vendor was accepted")
		}
		assertEmpty(t, s)
	})
}

func assertEmpty(t *testing.T, s *Store) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(s.Root, "store"))
	if len(entries) != 0 {
		t.Errorf("a refused download left %d entries in the store", len(entries))
	}
}

func TestNamesAreChecked(t *testing.T) {
	s := newStore(t)
	v := newVendor(t, []byte("x"))
	for _, tc := range []struct{ name, version, platform string }{
		{"../x", "1.0", "linux-arm64"},
		{"a/b", "1.0", "linux-arm64"},
		{"Tool", "1.0", "linux-arm64"},
		{"tool", "../1", "linux-arm64"},
		{"tool", "1.0", "linux/arm64"},
		{"tool", "", "linux-arm64"},
		{"", "1.0", "linux-arm64"},
	} {
		p := v.pin(sum([]byte("x")))
		p.Name, p.Version, p.Platform = tc.name, tc.version, tc.platform
		if _, err := s.Download(bg, p); !errors.Is(err, ErrBadName) {
			t.Errorf("%+v = %v, want ErrBadName", tc, err)
		}
	}
}

func TestAddFileAddsALocalBuild(t *testing.T) {
	s := newStore(t)
	file := filepath.Join(t.TempDir(), "whr-shim")
	if err := os.WriteFile(file, []byte("shim"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := s.AddFile("whr-shim", "v0.1.0", "linux-arm64", file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Dir, sum([]byte("shim"))[:8]+"-whr-shim-v0.1.0-linux-arm64") {
		t.Errorf("entry = %+v", e)
	}
	// Another build of the same version is another entry: content addressed.
	if err := os.WriteFile(file, []byte("shim2"), 0o600); err != nil {
		t.Fatal(err)
	}
	e2, err := s.AddFile("whr-shim", "v0.1.0", "linux-arm64", file)
	if err != nil || e2.Dir == e.Dir {
		t.Errorf("a changed build must be a new entry: %+v, %v", e2, err)
	}
}

func TestProfilesLinkToEntriesAndRollBack(t *testing.T) {
	s := newStore(t)
	mk := func(content, version string) Entry {
		f := filepath.Join(t.TempDir(), "tool")
		if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		e, err := s.AddFile("tool", version, "linux-arm64", f)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	v1, v2 := mk("one", "1.0"), mk("two", "2.0")
	if err := s.Profile("current", v1); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(s.Root, "profiles", "current", "bin", "tool")
	target, err := os.Readlink(link)
	if err != nil || filepath.IsAbs(target) {
		t.Fatalf("the profile link %q must be relative (%v)", target, err)
	}
	if got := readString(link); got != "one" {
		t.Errorf("profile resolves to %q", got)
	}
	// An upgrade is a profile change; a rollback is one too.
	if err := s.Profile("current", v2); err != nil {
		t.Fatal(err)
	}
	if got := readString(link); got != "two" {
		t.Errorf("after the upgrade the profile resolves to %q", got)
	}
	if err := s.Profile("current", v1); err != nil {
		t.Fatal(err)
	}
	if got := readString(link); got != "one" {
		t.Errorf("after the rollback the profile resolves to %q", got)
	}
	// Both entries are still in the store.
	if p := s.Verify(); len(p) != 0 {
		t.Errorf("verify: %v", p)
	}
	if err := s.Profile("../escape", v1); err == nil {
		t.Error("a profile name with a path was accepted")
	}
}

func TestVerifyNoticesATamperedOrWritableEntry(t *testing.T) {
	s := newStore(t)
	f := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(f, []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := s.AddFile("tool", "1.0", "linux-arm64", f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.Path(), 0o755); err != nil { //nolint:gosec // a test making the tool writable
		t.Fatal(err)
	}
	if p := joinProblems(s.Verify()); !strings.Contains(p, "writable") {
		t.Errorf("a writable tool was not noticed: %q", p)
	}
	if err := os.Chmod(filepath.Dir(e.Path()), 0o755); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if err := os.WriteFile(e.Path(), []byte("evil"), 0o755); err != nil { //nolint:gosec // a test tampering with the tool
		t.Fatal(err)
	}
	if p := joinProblems(s.Verify()); !strings.Contains(p, "but ") || !strings.Contains(p, "was recorded") {
		t.Errorf("a tampered tool was not noticed: %q", p)
	}
	makeWritable(s.Root)
}

func TestThePinsAreWellFormed(t *testing.T) {
	pins, err := Pins()
	if err != nil || len(pins) == 0 {
		t.Fatalf("pins = %v, %v", pins, err)
	}
	for _, p := range pins {
		if checkNames(p.Name, p.Version, p.Platform) != nil || !hashRe.MatchString(p.SHA256) || !strings.HasPrefix(p.BaseURL, "https://") {
			t.Errorf("pin %+v is not well formed", p)
		}
	}
}

// readString reads a file of the test store.
func readString(path string) string {
	b, _ := os.ReadFile(path) //nolint:gosec // a path in the test store
	return string(b)
}

func joinProblems(ps []Problem) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.String())
	}
	return strings.Join(out, "\n")
}

func addTool(t *testing.T, s *Store) Entry {
	t.Helper()
	f := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(f, []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := s.AddFile("tool", "1.0", "linux-arm64", f)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func severe(ps []Problem) bool {
	for _, p := range ps {
		if p.Severe {
			return true
		}
	}
	return false
}

// A change that keeps the first 32 bits of the hash (the eight digits in the name)
// is caught by the full hash: the name was only ever a label.
func TestVerifyComparesTheFullHashNotTheEightDigitsInTheName(t *testing.T) {
	s := newStore(t)
	e := addTool(t, s)
	makeWritable(e.Dir)
	if err := os.Chmod(e.Path(), 0o755); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	// Record another full hash that shares the eight digits of the name, as a
	// forged entry could: the binary no longer hashes to what is recorded.
	forged := e.SHA256[:8] + strings.Repeat("0", 56)
	if err := os.Chmod(filepath.Join(e.Dir, RecordedHashFile), 0o644); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.Dir, RecordedHashFile), []byte(forged+"\n"), 0o644); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if err := os.Chmod(e.Path(), 0o555); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if p := s.Verify(); !severe(p) || !strings.Contains(joinProblems(p), "was recorded") {
		t.Errorf("a binary that does not match the recorded full hash passed: %v", p)
	}
	makeWritable(s.Root)
}

func TestVerifyRefusesALinkInPlaceOfTheToolTheEntryOrTheRecord(t *testing.T) {
	// A symlink where the tool should be: its content may be what was installed,
	// but a tool is never a link, and it must not be followed.
	s := newStore(t)
	e := addTool(t, s)
	makeWritable(e.Dir)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(elsewhere, []byte("good"), 0o755); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if err := os.Remove(e.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, e.Path()); err != nil {
		t.Fatal(err)
	}
	if p := s.Verify(); !severe(p) || !strings.Contains(joinProblems(p), "not a regular file") {
		t.Errorf("a link in place of the tool passed: %v", p)
	}
	makeWritable(s.Root)

	// The entry itself a link to a directory elsewhere.
	s = newStore(t)
	e = addTool(t, s)
	moved := filepath.Join(t.TempDir(), "real-entry")
	makeWritable(e.Dir)
	if err := os.Rename(e.Dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, e.Dir); err != nil {
		t.Fatal(err)
	}
	if p := s.Verify(); !severe(p) || !strings.Contains(joinProblems(p), "not a store entry") {
		t.Errorf("an entry that is a link passed: %v", p)
	}
	makeWritable(moved)

	// The record a link to a file that says the right thing.
	s = newStore(t)
	e = addTool(t, s)
	makeWritable(e.Dir)
	rec := filepath.Join(t.TempDir(), "rec")
	if err := os.WriteFile(rec, []byte(e.SHA256+"\n"), 0o644); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.Dir, RecordedHashFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rec, filepath.Join(e.Dir, RecordedHashFile)); err != nil {
		t.Fatal(err)
	}
	if p := s.Verify(); !severe(p) || !strings.Contains(joinProblems(p), RecordedHashFile) {
		t.Errorf("a record that is a link passed: %v", p)
	}
	makeWritable(s.Root)
}

// An entry from before the full hash was recorded is checked against its pin if it
// has one, and only against the name's eight digits, with a problem that is not
// severe, if it has none.
func TestVerifyHandlesAnEntryWithoutARecordedHash(t *testing.T) {
	s := newStore(t)
	e := addTool(t, s)
	makeWritable(e.Dir)
	if err := os.Remove(filepath.Join(e.Dir, RecordedHashFile)); err != nil {
		t.Fatal(err)
	}
	p := s.Verify()
	if len(p) != 1 || p[0].Severe || !strings.Contains(p[0].Msg, "32 bits") {
		t.Errorf("an older entry with no pin: %v", p)
	}
	// With the first digits right and the rest forged, nothing but a pin can tell, and
	// there is none: that is the problem's point. With a pin, it is caught.
	pins, err := Pins()
	if err != nil || len(pins) == 0 {
		t.Skip("no built-in pin")
	}
	pin := pins[0]
	dir := filepath.Join(s.Root, "store", fmt.Sprintf("%s-%s-%s-%s", pin.SHA256[:8], pin.Name, pin.Version, pin.Platform))
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", pin.Name), []byte("not the pinned content"), 0o555); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if got := joinProblems(s.Verify()); !strings.Contains(got, "the pin says") {
		t.Errorf("an entry that does not match its pin passed: %s", got)
	}
	makeWritable(s.Root)
}

// The record and the tool are written by the same user, so a tool and a record
// changed together agree with each other; only the pin, compiled into the binary,
// catches it.
func TestAPinDecidesEvenWhenTheRecordAgreesWithATamperedTool(t *testing.T) {
	pins, err := Pins()
	if err != nil || len(pins) == 0 {
		t.Skip("no built-in pin")
	}
	pin := pins[0]
	s := newStore(t)
	dir := filepath.Join(s.Root, "store", fmt.Sprintf("%s-%s-%s-%s", pin.SHA256[:8], pin.Name, pin.Version, pin.Platform))
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	evil := []byte("a tampered tool")
	if err := os.WriteFile(filepath.Join(dir, "bin", pin.Name), evil, 0o555); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	sum := sha256.Sum256(evil)
	// A record in the entry, valid in form (a SHA-256 that starts with the name's
	// digits), as whoever changed the tool could have written it.
	forged := pin.SHA256[:8] + hex.EncodeToString(sum[:])[8:]
	if err := os.WriteFile(filepath.Join(dir, RecordedHashFile), []byte(forged+"\n"), 0o444); err != nil { //nolint:gosec // a test
		t.Fatal(err)
	}
	if got := joinProblems(s.Verify()); !strings.Contains(got, "the pin says") {
		t.Errorf("a tampered pinned tool was not caught against its pin: %s", got)
	}
	makeWritable(s.Root)
}
