package toolstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FormatCodex identifies Codex's standalone release archive. Provenance is
// checked externally when admitting a pin (testdata/README.md); the supervisor
// trusts the reviewed archive and binary SHA-256 pins only, like Antigravity.
const FormatCodex = "codex"

func (s *Store) downloadCodex(ctx context.Context, p Pin) (Entry, error) {
	if p.Name != "codex" || p.Platform != "linux-arm64-musl" {
		return Entry{}, fmt.Errorf("%w: Codex supports the pinned arm64 musl artifact only", ErrBadName)
	}
	if !hashRe.MatchString(p.ArchiveSHA256) {
		return Entry{}, fmt.Errorf("%w: no archive SHA-256", ErrChecksum)
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
	if got, err := hashFile(archive); err != nil {
		return Entry{}, err
	} else if got != p.ArchiveSHA256 {
		return Entry{}, ErrChecksum
	}
	file := filepath.Join(tmp, "codex")
	if err := extractTarGzFile(archive, "codex-aarch64-unknown-linux-musl", s.maxBytes(), file); err != nil {
		return Entry{}, err
	}
	digest, err := hashFile(file)
	if err != nil {
		return Entry{}, err
	}
	if digest != p.SHA256 {
		return Entry{}, ErrChecksum
	}
	// install rechecks the copied bytes against the admitted binary pin.
	return s.install(p.Name, p.Version, p.Platform, file, digest)
}
