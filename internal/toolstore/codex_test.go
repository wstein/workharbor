package toolstore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexFixtureDigest = "113aa5d5952a6fd3950a88323219747d0c91978ebee948509a540c8a460e59a3"

func TestCodexRuntimeUsesAdmittedPinOnly(t *testing.T) {
	tool := []byte("externally admitted bytes")
	archive := makeTarGz(t, tarEntry{name: "codex-aarch64-unknown-linux-musl", body: tool})
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/archive" {
			t.Errorf("runtime requested unexpected evidence %s", r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	p := Pin{Name: "codex", Version: "0.159.2", Platform: "linux-arm64-musl", Format: FormatCodex, SHA256: sum(tool), ArchiveURL: srv.URL + "/archive", ArchiveSHA256: sum(archive)}
	s := newStore(t)
	e, err := s.Download(bg, p)
	if err != nil {
		t.Fatal(err)
	}
	if e.SHA256 != sum(tool) || readString(e.Path()) != string(tool) {
		t.Fatalf("unexpected installed entry %+v", e)
	}
	if requests != 1 {
		t.Fatalf("runtime made %d requests, want archive only", requests)
	}
}

func TestCodexDownloadRefusesAndStoresNothing(t *testing.T) {
	tool := []byte("pinned binary")
	good := makeTarGz(t, tarEntry{name: "codex-aarch64-unknown-linux-musl", body: tool})
	for _, tc := range []struct {
		name    string
		archive []byte
		mutate  func(*Pin)
		want    error
	}{
		{"archive changed", good, func(p *Pin) { p.ArchiveSHA256 = sum([]byte("different")) }, ErrChecksum},
		{"binary changed", good, func(p *Pin) { p.SHA256 = sum([]byte("different")) }, ErrChecksum},
		{"missing archive hash", good, func(p *Pin) { p.ArchiveSHA256 = "" }, ErrChecksum},
		{"unsafe archive", makeTarGz(t, tarEntry{name: "../escape", body: tool}), nil, ErrArchive},
		{"glibc has no admitted pin", good, func(p *Pin) { p.Platform = "linux-arm64" }, ErrBadName},
		{"other tool", good, func(p *Pin) { p.Name = "other" }, ErrBadName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(tc.archive) }))
			defer srv.Close()
			p := Pin{Name: "codex", Version: "0.159.2", Platform: "linux-arm64-musl", Format: FormatCodex, SHA256: sum(tool), ArchiveURL: srv.URL + "/archive", ArchiveSHA256: sum(tc.archive)}
			if tc.mutate != nil {
				tc.mutate(&p)
			}
			s := newStore(t)
			if _, err := s.Download(bg, p); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			assertEmpty(t, s)
		})
	}
}

func TestCodexRefusesPlainHTTP(t *testing.T) {
	p := Pin{Name: "codex", Version: "0.159.2", Platform: "linux-arm64-musl", Format: FormatCodex, SHA256: sum([]byte("x")), ArchiveURL: "http://example.invalid/archive", ArchiveSHA256: sum([]byte("x"))}
	s := newStore(t)
	s.allowHTTP = false
	if _, err := s.Download(bg, p); !errors.Is(err, ErrInsecure) {
		t.Fatalf("got %v", err)
	}
	assertEmpty(t, s)
}

func TestExtractBoundsSkippedPayload(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	raw := makeTarGz(t, tarEntry{name: "other", body: make([]byte, 100)}, tarEntry{name: "codex", body: []byte("x")})
	if err := os.WriteFile(archive, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGzFile(archive, "codex", 50, filepath.Join(dir, "out")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}

func TestBuiltInCodexPin(t *testing.T) {
	pins, err := Pins()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range pins {
		if p.Name != "codex" {
			continue
		}
		count++
		if p.Platform != "linux-arm64-musl" || p.Format != FormatCodex || p.Version != "0.159.2" || p.SHA256 != codexFixtureDigest || !hashRe.MatchString(p.ArchiveSHA256) || !strings.HasPrefix(p.ArchiveURL, "https://github.com/openai/codex/releases/download/rust-v"+p.Version+"/") {
			t.Fatalf("unexpected pin %+v", p)
		}
	}
	if count != 1 {
		t.Fatalf("got %d Codex pins; upstream supplies only arm64 musl", count)
	}
}

// This optional artifact check exercises the runtime digest/extraction/install
// path against the public archive. Provenance is admitted externally, as
// documented in testdata/README.md. This test never executes Codex.
func TestCodexReleaseArchive(t *testing.T) {
	archivePath := os.Getenv("WHR_CODEX_RELEASE_ARCHIVE")
	if archivePath == "" {
		t.Skip("set WHR_CODEX_RELEASE_ARCHIVE to the pinned public tarball")
	}
	archive, err := os.ReadFile(archivePath) //nolint:gosec // the operator names a public artifact; it is never executed
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	defer srv.Close()
	pins, err := Pins()
	if err != nil {
		t.Fatal(err)
	}
	var p Pin
	for _, pin := range pins {
		if pin.Name == "codex" {
			p = pin
		}
	}
	p.ArchiveURL = srv.URL + "/archive"
	s := newStore(t)
	e, err := s.Download(bg, p)
	if err != nil {
		t.Fatal(err)
	}
	if e.SHA256 != codexFixtureDigest {
		t.Fatalf("got digest %s", e.SHA256)
	}
	if problems := s.Verify(); len(problems) != 0 {
		t.Fatalf("store verification: %v", problems)
	}
}
