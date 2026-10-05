package toolstore

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strings"
)

// ErrArchive is a release archive that is not safe to extract.
var ErrArchive = errors.New("the archive is not safe to extract")

// maxArchiveEntries caps the entries of a release archive: a tool archive holds
// the tool and perhaps a licence, so more is not a release.
const maxArchiveEntries = 64

// extractTarGzFile writes the one regular file named want out of a .tar.gz at
// archive to dst (a fresh path), at most maxBytes of it. Nothing else is
// extracted, and the archive as a whole must be plain: every entry is a regular
// file or a directory, with a relative name that stays inside the archive (no
// absolute path, no ".."), so a link, a device, a FIFO or a path that escapes
// refuses the archive even though only one file is written. The file must occur
// exactly once. Where the bytes go is decided by the caller, never by the
// archive.
func extractTarGzFile(archive, want string, maxBytes int64, dst string) error {
	f, err := os.Open(archive) //nolint:gosec // a file we just downloaded into our own temp directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}
	defer func() { _ = gz.Close() }()
	// Bound all decompressed bytes, including skipped files and tar metadata.
	// Tar headers/padding get a fixed allowance; payloads share maxBytes.
	const overhead = maxArchiveEntries*2048 + 1024
	if maxBytes <= 0 || maxBytes > math.MaxInt64-overhead-1 {
		return ErrTooLarge
	}
	expanded := &io.LimitedReader{R: gz, N: maxBytes + overhead + 1}
	tr := tar.NewReader(expanded)
	var total int64
	found := false
	for n := 0; ; n++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrArchive, err)
		}
		if n >= maxArchiveEntries {
			return fmt.Errorf("%w: more than %d entries", ErrArchive, maxArchiveEntries)
		}
		if h.Size < 0 || h.Size > maxBytes-total {
			return fmt.Errorf("%w: archive payload exceeds %d bytes", ErrTooLarge, maxBytes)
		}
		total += h.Size
		name := strings.TrimPrefix(h.Name, "./")
		if name == "" || path.IsAbs(h.Name) || strings.Contains(h.Name, "\\") || strings.ContainsRune(h.Name, 0) {
			return fmt.Errorf("%w: entry name %q", ErrArchive, h.Name)
		}
		for _, seg := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
			if seg == ".." || seg == "" {
				return fmt.Errorf("%w: entry name %q leaves the archive", ErrArchive, h.Name)
			}
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return fmt.Errorf("%w: entry %q is not a regular file or directory (type %q): links and special files are refused", ErrArchive, h.Name, h.Typeflag)
		}
		if name != want {
			continue
		}
		if found {
			return fmt.Errorf("%w: %q occurs twice", ErrArchive, want)
		}
		if h.Size < 0 || h.Size > maxBytes {
			return fmt.Errorf("%w: %q is %d bytes, the limit is %d", ErrTooLarge, want, h.Size, maxBytes)
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700) //nolint:gosec // a fresh file in our own temp directory
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(tr, maxBytes+1))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrArchive, err)
		}
		if n > maxBytes {
			return fmt.Errorf("%w: %q is larger than %d bytes", ErrTooLarge, want, maxBytes)
		}
		found = true
	}
	// Consume padding/trailing gzip data to check the gzip checksum and enforce
	// the expansion cap even when tar ended before the compressed stream.
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}
	if expanded.N == 0 {
		return ErrTooLarge
	}
	if !found {
		return fmt.Errorf("%w: no %q in the archive", ErrArchive, want)
	}
	return nil
}
