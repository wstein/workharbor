package toolstore

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FormatAntigravity is the Pin.Format of a tool released as the installer of
// Antigravity's CLI gets it: a manifest <base>/<platform with _>.json naming a
// .tar.gz and its SHA-512. The manifest host is the installer's own download
// address, not a documented API (spike musl-cli); it may change without notice,
// which is why the pin carries the archive URL and both hashes, and a change
// there fails the download instead of following it.
const FormatAntigravity = "antigravity"

var sha512Re = regexp.MustCompile(`^[0-9a-f]{128}$`)

// downloadAntigravity checks the pin, the vendor manifest and the archive
// against one another: the manifest must name the pin's version, archive URL
// and SHA-512, the downloaded archive must hash to the pin's SHA-512, and the
// tool extracted from it must hash to the pin's SHA-256, the hash the store
// keeps. Any mismatch is ErrChecksum and nothing is stored.
func (s *Store) downloadAntigravity(ctx context.Context, p Pin) (Entry, error) {
	if !sha512Re.MatchString(p.SHA512) {
		return Entry{}, fmt.Errorf("%w: the pin has no SHA-512", ErrChecksum)
	}
	base := strings.TrimRight(p.BaseURL, "/")
	for _, u := range []string{base, p.ArchiveURL} {
		if err := s.checkScheme(u); err != nil {
			return Entry{}, err
		}
	}
	got, err := s.antigravityManifest(ctx, base+"/"+strings.ReplaceAll(p.Platform, "-", "_")+".json")
	if err != nil {
		return Entry{}, err
	}
	if got.Version != p.Version || got.URL != p.ArchiveURL || got.SHA512 != p.SHA512 {
		return Entry{}, fmt.Errorf("%w: the vendor manifest says version %s, %s, sha512 %.16s (first 16 digits), the pin says %s, %s, %.16s", ErrChecksum, got.Version, got.URL, got.SHA512, p.Version, p.ArchiveURL, p.SHA512)
	}
	if err := os.MkdirAll(filepath.Join(s.Root, "store"), 0o750); err != nil {
		return Entry{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Join(s.Root, "store"), ".download-")
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	archive := filepath.Join(tmp, "archive.tar.gz")
	if err := s.fetch(ctx, p.ArchiveURL, archive); err != nil {
		return Entry{}, err
	}
	sum, err := hashSHA512(archive)
	if err != nil {
		return Entry{}, err
	}
	if sum != p.SHA512 {
		return Entry{}, fmt.Errorf("%w: the archive hashes to sha512 %.16s (first 16 digits), the pin says %.16s", ErrChecksum, sum, p.SHA512)
	}
	file := filepath.Join(tmp, p.Name)
	if err := extractTarGzFile(archive, p.Name, s.maxBytes(), file); err != nil {
		return Entry{}, err
	}
	return s.install(p.Name, p.Version, p.Platform, file, p.SHA256)
}

type antigravityManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA512  string `json:"sha512"`
}

func (s *Store) antigravityManifest(ctx context.Context, src string) (antigravityManifest, error) {
	var m antigravityManifest
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return m, err
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return m, fmt.Errorf("toolstore: the vendor manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := s.checkScheme(resp.Request.URL.String()); err != nil {
		return m, err
	}
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("toolstore: the vendor manifest answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return m, fmt.Errorf("toolstore: the vendor manifest: %w", err)
	}
	if !sha512Re.MatchString(m.SHA512) {
		return m, fmt.Errorf("%w: the vendor manifest has no SHA-512", ErrChecksum)
	}
	return m, nil
}

func hashSHA512(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // a file we just downloaded into our own temp directory
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
