package toolstore

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// FormatAntigravity is the Pin.Format of a tool released as the installer of
// Antigravity's CLI gets it: a .tar.gz and its SHA-512, listed in a manifest
// <base>/<platform with _>.json. The manifest host is the installer's own
// download address, not a documented API (spike musl-cli); it may change
// without notice, which is why the pin carries the archive URL and both hashes
// and the build never reads the manifest.
const FormatAntigravity = "antigravity"

var sha512Re = regexp.MustCompile(`^[0-9a-f]{128}$`)

// downloadAntigravity trusts the pin alone: the vendor's manifest names only
// the latest release, so fetching it at run time would break a build the day a
// newer version appears. The pinned https archive URL must deliver an archive
// that hashes to the pin's SHA-512, and the tool extracted from it must hash to
// the pin's SHA-256, the hash the store keeps. Any mismatch is ErrChecksum and
// nothing is stored. The manifest is compared with the pin only when a pin is
// written or updated (scripts/antigravity-pin-check.sh).
func (s *Store) downloadAntigravity(ctx context.Context, p Pin) (Entry, error) {
	if !sha512Re.MatchString(p.SHA512) {
		return Entry{}, fmt.Errorf("%w: the pin has no SHA-512", ErrChecksum)
	}
	if err := s.checkScheme(p.ArchiveURL); err != nil {
		return Entry{}, err
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
