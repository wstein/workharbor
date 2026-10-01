package devcontainer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxFileBytes is the largest toolchain or Dockerfile the reader takes into
// memory.
const maxFileBytes = 1 << 20

// ErrNotFound is returned when a wanted file is not in the tree.
var ErrNotFound = errors.New("devcontainer: not in the repository")

func checkRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, ": \t\n\r\x00") || strings.Contains(ref, "..") {
		return fmt.Errorf("%w: %q", ErrBadRef, ref)
	}
	return nil
}

// entry is one line of ls-tree: a path in a tree with its mode and object.
type entry struct {
	Mode, Type, OID, Path string
}

func (e entry) regular() bool { return e.Mode == "100644" || e.Mode == "100755" }

// lsTree lists a path of the tree of ref (the whole tree when p is empty),
// recursively or not. A path that is not there lists nothing.
func lsTree(ctx context.Context, r Runner, ref, p string, recursive bool) ([]entry, error) {
	args := []string{"ls-tree", "-z", "--full-tree"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, "--end-of-options", ref)
	if p != "" {
		args = append(args, "--", p)
	}
	out, err := r.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("devcontainer: list %q at %s: %w", p, ref, err)
	}
	var list []entry
	for _, rec := range bytes.Split(out, []byte{0}) {
		meta, name, ok := strings.Cut(string(rec), "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 3 {
			return nil, fmt.Errorf("devcontainer: unexpected ls-tree line %q", rec)
		}
		list = append(list, entry{Mode: f[0], Type: f[1], OID: f[2], Path: name})
	}
	return list, nil
}

// file reads one regular file of the tree. It returns ErrNotFound when the
// path is absent, and ErrRefused when it is not a regular file (a symbolic link
// in the repository could name anything on the host), or is larger than
// maxFileBytes.
func file(ctx context.Context, r Runner, ref, p string) ([]byte, error) {
	list, err := lsTree(ctx, r, ref, p, false)
	if err != nil {
		return nil, err
	}
	if len(list) != 1 || list[0].Path != p {
		return nil, ErrNotFound
	}
	if !list[0].regular() {
		return nil, fmt.Errorf("%w: %s at %s is not a regular file (mode %s)", ErrRefused, p, ref, list[0].Mode)
	}
	data, err := r.Run(ctx, "cat-file", "blob", list[0].OID)
	if err != nil {
		return nil, fmt.Errorf("devcontainer: read %s at %s: %w", p, ref, err)
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrRefused, p, maxFileBytes)
	}
	return data, nil
}

// Limits bound what Export writes, so a repository cannot fill the disk.
type Limits struct {
	Files int
	Bytes int64
}

// DefaultLimits are the limits Export uses for a zero Limits.
var DefaultLimits = Limits{Files: 20000, Bytes: 512 << 20}

// Exported is what Export wrote.
type Exported struct {
	Files   int
	Bytes   int64
	Skipped []string // symbolic links and submodules, which are never exported
}

// Export writes the tree below dir of commit ref into dest, from the
// repository's objects, never from a working tree: a build context is exactly
// what was reviewed, with no untracked file, no .git and no filter or
// attribute applied. dir "." is the whole tree. A symbolic link or a submodule
// is skipped and listed, because a link in a context could be followed out of it.
func Export(ctx context.Context, r Runner, ref, dir, dest string, lim Limits) (Exported, error) {
	if lim.Files == 0 && lim.Bytes == 0 {
		lim = DefaultLimits
	}
	if err := checkRef(ref); err != nil {
		return Exported{}, err
	}
	dir = path.Clean(dir)
	if dir != "." && !filepath.IsLocal(dir) {
		return Exported{}, fmt.Errorf("%w: build context %q leaves the repository", ErrRefused, dir)
	}
	pathspec := dir
	if dir == "." {
		pathspec = ""
	}
	list, err := lsTree(ctx, r, ref, pathspec, true)
	if err != nil {
		return Exported{}, err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return Exported{}, err
	}
	var res Exported
	for _, e := range list {
		rel := e.Path
		if dir != "." {
			rel = strings.TrimPrefix(e.Path, dir+"/")
		}
		if e.Type != "blob" || !e.regular() {
			res.Skipped = append(res.Skipped, e.Path+" (mode "+e.Mode+")")
			continue
		}
		if !filepath.IsLocal(rel) {
			return res, fmt.Errorf("%w: %q is not a path inside the context", ErrRefused, e.Path)
		}
		if res.Files++; res.Files > lim.Files {
			return res, fmt.Errorf("%w: the context has more than %d files", ErrRefused, lim.Files)
		}
		data, err := r.Run(ctx, "cat-file", "blob", e.OID)
		if err != nil {
			return res, fmt.Errorf("devcontainer: read %s: %w", e.Path, err)
		}
		if res.Bytes += int64(len(data)); res.Bytes > lim.Bytes {
			return res, fmt.Errorf("%w: the context is larger than %d bytes", ErrRefused, lim.Bytes)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return res, err
		}
		mode := os.FileMode(0o600)
		if e.Mode == "100755" {
			mode = 0o700
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return res, err
		}
	}
	return res, nil
}
