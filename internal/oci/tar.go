package oci

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ExtractLimits bound what a feature archive may unpack to.
type ExtractLimits struct {
	Files int   // entries: regular files and directories alike
	Bytes int64 // total bytes of regular files
	Depth int   // path elements
}

// DefaultExtractLimits are generous for a feature (a script and a few files) and
// small against a bomb.
var DefaultExtractLimits = ExtractLimits{Files: 2000, Bytes: 64 << 20, Depth: 12}

// ErrUnsafeArchive is returned for an entry that could write outside the directory or
// create anything but a plain file or directory.
var ErrUnsafeArchive = errors.New("oci: the archive holds something unsafe")

// Extract unpacks a tar (plain or gzip) into dir, which must exist and be empty or
// owned by the caller. It accepts only regular files and directories: an absolute
// path, a ".." element, a backslash, a NUL, a symbolic or hard link, a device, a
// fifo or a duplicate path is an error, and so is exceeding the limits. Files are
// written 0644, or 0755 if the archive marks them executable, never with another
// owner or mode, and never through an existing link.
func Extract(r io.Reader, dir string, lim ExtractLimits) error {
	if lim.Files <= 0 {
		lim = DefaultExtractLimits
	}
	br := bufio.NewReader(r)
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnsafeArchive, err) //nolint:errorlint // the cause is detail
		}
		defer func() { _ = gz.Close() }()
		r = gz
	} else {
		r = br
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	seen := map[string]bool{}
	var entries int
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnsafeArchive, err) //nolint:errorlint // the cause is detail
		}
		name, err := cleanName(h.Name, lim.Depth)
		if err != nil {
			return err
		}
		if name == "." {
			continue
		}
		if seen[name] {
			return fmt.Errorf("%w: %q appears twice", ErrUnsafeArchive, h.Name)
		}
		seen[name] = true
		// every entry counts, directories too: 16 MiB of gzip holds hundreds of thousands
		if entries++; entries > lim.Files {
			return fmt.Errorf("%w: more than %d entries", ErrUnsafeArchive, lim.Files)
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil { //nolint:gosec // a directory of an extracted feature, readable by the build
				return err
			}
		case tar.TypeReg:
			if h.Size < 0 || total+h.Size > lim.Bytes {
				return fmt.Errorf("%w: more than %d bytes", ErrUnsafeArchive, lim.Bytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { //nolint:gosec // as above
				return err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // target was cleaned and is under root; O_EXCL refuses an existing path or link
			if err != nil {
				return err
			}
			n, cerr := io.Copy(f, io.LimitReader(tr, h.Size))
			if err := f.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
			if n != h.Size {
				return fmt.Errorf("%w: %q is shorter than its header says", ErrUnsafeArchive, h.Name)
			}
			total += n
		default:
			return fmt.Errorf("%w: %q is a %s, only files and directories are accepted", ErrUnsafeArchive, h.Name, typeName(h.Typeflag))
		}
	}
}

func typeName(t byte) string {
	switch t {
	case tar.TypeSymlink:
		return "symbolic link"
	case tar.TypeLink:
		return "hard link"
	case tar.TypeChar, tar.TypeBlock:
		return "device"
	case tar.TypeFifo:
		return "fifo"
	}
	return "special entry"
}

// cleanName turns an archive path into a clean relative path, or refuses it.
func cleanName(name string, depth int) (string, error) {
	if name == "" || strings.ContainsAny(name, "\x00\\") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("%w: %q", ErrUnsafeArchive, name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return ".", nil
	}
	for _, el := range strings.Split(clean, "/") {
		if el == ".." || el == "" {
			return "", fmt.Errorf("%w: %q", ErrUnsafeArchive, name)
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(clean)) || len(clean) > 1024 || strings.Count(clean, "/")+1 > depth {
		return "", fmt.Errorf("%w: %q is not a short local path", ErrUnsafeArchive, name)
	}
	return clean, nil
}
