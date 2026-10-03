package toolstore

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
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

type tarEntry struct {
	name, link string
	typ        byte
	body       []byte
}

func makeTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if e.typ == 0 {
			e.typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o755, Linkname: e.link}
		if e.typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			_, _ = tw.Write(e.body)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum512(b []byte) string { h := sha512.Sum512(b); return hex.EncodeToString(h[:]) }

// agy is a fake Antigravity release host: a manifest and an archive.
type agy struct {
	srv          *httptest.Server
	archive      []byte
	claimed      string // the manifest's sha512
	version      string
	archHits     int
	manifestHits int
}

func newAgy(t *testing.T, archive []byte) *agy {
	t.Helper()
	a := &agy{archive: archive, claimed: sum512(archive), version: "1.2.3"}
	mux := http.NewServeMux()
	mux.HandleFunc("/manifests/linux_arm64_musl.json", func(w http.ResponseWriter, _ *http.Request) {
		a.manifestHits++
		fmt.Fprintf(w, `{"version":%q,"url":%q,"sha512":%q}`, a.version, a.srv.URL+"/dl/cli.tar.gz", a.claimed)
	})
	mux.HandleFunc("/dl/cli.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		a.archHits++
		_, _ = w.Write(a.archive)
	})
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *agy) pin(tool []byte) Pin {
	return Pin{
		Name: "antigravity", Version: "1.2.3", Platform: "linux-arm64-musl", BaseURL: a.srv.URL + "/manifests",
		SHA256: sum(tool), Format: FormatAntigravity, ArchiveURL: a.srv.URL + "/dl/cli.tar.gz", SHA512: sum512(a.archive),
	}
}

func TestAntigravityDownloadIsVerifiedAndStored(t *testing.T) {
	tool := []byte("agy binary")
	a := newAgy(t, makeTarGz(t, tarEntry{name: "antigravity", body: tool}))
	s := newStore(t)
	e, err := s.Download(bg, a.pin(tool))
	if err != nil {
		t.Fatal(err)
	}
	if e.SHA256 != sum(tool) || readString(e.Path()) != string(tool) {
		t.Errorf("entry %+v", e)
	}
}

// The vendor manifest names only the latest release: a build never reads it,
// so a newer version there cannot break the pinned one.
func TestAntigravityBuildIgnoresTheManifest(t *testing.T) {
	tool := []byte("agy binary")
	a := newAgy(t, makeTarGz(t, tarEntry{name: "antigravity", body: tool}))
	a.version, a.claimed = "9.9.9", sum512([]byte("newer"))
	if _, err := newStore(t).Download(bg, a.pin(tool)); err != nil {
		t.Fatal(err)
	}
	if a.manifestHits != 0 {
		t.Errorf("the build fetched the manifest %d times", a.manifestHits)
	}
}

func TestAntigravityMismatchesStoreNothing(t *testing.T) {
	tool := []byte("agy binary")
	good := makeTarGz(t, tarEntry{name: "antigravity", body: tool})
	other := makeTarGz(t, tarEntry{name: "antigravity", body: []byte("evil")})
	for name, mutate := range map[string]func(a *agy, p *Pin){
		"archive differs from both":      func(a *agy, _ *Pin) { a.archive = other },
		"pin sha256 of the tool differs": func(_ *agy, p *Pin) { p.SHA256 = sum([]byte("else")) },
		"pin has no sha512":              func(_ *agy, p *Pin) { p.SHA512 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			a := newAgy(t, good)
			p := a.pin(tool)
			mutate(a, &p)
			s := newStore(t)
			if _, err := s.Download(bg, p); !errors.Is(err, ErrChecksum) {
				t.Fatalf("err = %v, want ErrChecksum", err)
			}
			assertEmpty(t, s)
		})
	}
}

func TestAntigravityRefusesPlainHTTP(t *testing.T) {
	tool := []byte("x")
	a := newAgy(t, makeTarGz(t, tarEntry{name: "antigravity", body: tool}))
	s := newStore(t)
	s.allowHTTP = false
	if _, err := s.Download(bg, a.pin(tool)); !errors.Is(err, ErrInsecure) {
		t.Fatalf("err = %v", err)
	}
}

func TestExtractRefusesUnsafeArchives(t *testing.T) {
	body := []byte("tool")
	for name, tc := range map[string]struct {
		entries []tarEntry
		want    error
	}{
		"path escape":      {[]tarEntry{{name: "../antigravity", body: body}}, ErrArchive},
		"escape elsewhere": {[]tarEntry{{name: "antigravity", body: body}, {name: "a/../../x", body: body}}, ErrArchive},
		"absolute":         {[]tarEntry{{name: "/etc/passwd", body: body}}, ErrArchive},
		"symlink":          {[]tarEntry{{name: "antigravity", body: body}, {name: "l", typ: tar.TypeSymlink, link: "/etc/passwd"}}, ErrArchive},
		"hardlink":         {[]tarEntry{{name: "antigravity", body: body}, {name: "h", typ: tar.TypeLink, link: "antigravity"}}, ErrArchive},
		"fifo":             {[]tarEntry{{name: "f", typ: tar.TypeFifo}}, ErrArchive},
		"missing":          {[]tarEntry{{name: "other", body: body}}, ErrArchive},
		"twice":            {[]tarEntry{{name: "antigravity", body: body}, {name: "./antigravity", body: body}}, ErrArchive},
		"too large":        {[]tarEntry{{name: "antigravity", body: bytes.Repeat([]byte{1}, 100)}}, ErrTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, "a.tar.gz")
			if err := os.WriteFile(arc, makeTarGz(t, tc.entries...), 0o600); err != nil {
				t.Fatal(err)
			}
			err := extractTarGzFile(arc, "antigravity", 50, filepath.Join(dir, "out"))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if errors.Is(tc.want, ErrTooLarge) {
				if err := extractTarGzFile(arc, "antigravity", 99, filepath.Join(dir, "out2")); !errors.Is(err, ErrTooLarge) {
					t.Errorf("limit 99: %v", err)
				}
				if err := extractTarGzFile(arc, "antigravity", 100, filepath.Join(dir, "out3")); err != nil {
					t.Errorf("limit 100: %v", err)
				}
			}
		})
	}
}

func TestExtractTooManyEntries(t *testing.T) {
	var es []tarEntry
	for i := 0; i < maxArchiveEntries+1; i++ {
		es = append(es, tarEntry{name: fmt.Sprintf("f%d", i), body: []byte("x")})
	}
	dir := t.TempDir()
	arc := filepath.Join(dir, "a.tar.gz")
	_ = os.WriteFile(arc, makeTarGz(t, es...), 0o600)
	if err := extractTarGzFile(arc, "f0", 10, filepath.Join(dir, "o")); !errors.Is(err, ErrArchive) || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuiltInAntigravityPinsAreWellFormed(t *testing.T) {
	pins, _ := Pins()
	n := 0
	for _, p := range pins {
		if p.Name != "antigravity" {
			continue
		}
		n++
		if p.Format != FormatAntigravity || !sha512Re.MatchString(p.SHA512) || !hashRe.MatchString(p.SHA256) ||
			!strings.HasPrefix(p.ArchiveURL, "https://") || !strings.HasPrefix(p.BaseURL, "https://") {
			t.Errorf("pin %+v", p)
		}
	}
	if n != 2 {
		t.Errorf("%d antigravity pins, want glibc and musl", n)
	}
}
