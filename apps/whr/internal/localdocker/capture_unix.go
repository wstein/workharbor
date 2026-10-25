// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const stageMaxEntries = 100000
const stageMaxWorkingBytes = int64(2 << 30)
const stageMaxFileBytes = int64(64 << 20)

type capturedStageFiles struct {
	Path, Source, Digest string
	Omitted              []string
	Entries              []map[string]any
}

func (c *capturedStageFiles) Close() {
	if c != nil && c.Path != "" {
		os.RemoveAll(c.Path)
		c.Path = ""
	}
}
func (c *capturedStageFiles) Check(ctx context.Context) error {
	entries, omitted, e := scanStageFiles(ctx, c.Source, "")
	if e != nil {
		return e
	}
	if captureDigest(c.Source, entries, omitted) != c.Digest {
		return stageConflict("source inputs changed since capture")
	}
	return nil
}
func captureStageFiles(ctx context.Context, source, parent string) (*capturedStageFiles, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	physical, e := filepath.EvalSymlinks(source)
	if e != nil {
		return nil, stageConflict("source path unavailable")
	}
	physical, e = filepath.Abs(physical)
	if e != nil {
		return nil, stageConflict("source path unavailable")
	}
	out, e := os.MkdirTemp(parent, "capture-")
	if e != nil {
		return nil, stageConflict("capture storage unavailable")
	}
	if e = os.Chmod(out, 0700); e != nil {
		os.RemoveAll(out)
		return nil, stageConflict("capture storage mode")
	}
	c := &capturedStageFiles{Path: out, Source: physical}
	c.Entries, c.Omitted, e = scanStageFiles(ctx, physical, out)
	if e != nil {
		c.Close()
		return nil, e
	}
	c.Digest = captureDigest(physical, c.Entries, c.Omitted)
	if e = c.Check(ctx); e != nil {
		c.Close()
		return nil, e
	}
	return c, nil
}
func captureDigest(source string, entries []map[string]any, omitted []string) string {
	// encoding/json sorts map keys. Every record is a map so canonical ordering
	// does not accidentally depend on Go struct field declaration order.
	b, _ := json.Marshal(map[string]any{"entries": entries, "omitted": omitted, "source": source})
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func stageOmit(path string) bool {
	name := filepath.Base(path)
	return name == ".git" || name == ".ssh" || name == ".aws" || name == ".gnupg" || name == ".docker" || name == ".netrc" || name == ".npmrc" || name == ".pypirc" || name == ".git-credentials" || name == ".env" || strings.HasPrefix(name, ".env.") || path == ".devcontainer/host.env" || path == ".devcontainer/smoke.env"
}
func scanStageFiles(ctx context.Context, source, destination string) ([]map[string]any, []string, error) {
	root, e := os.OpenRoot(source)
	if e != nil {
		return nil, nil, stageConflict("source root unavailable")
	}
	defer root.Close()
	records := []map[string]any{}
	omitted := []string{}
	var working int64
	count := 0
	fail := func(reason string) error { return stageConflict("capture " + reason) }
	e = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return fail("enumeration changed or unavailable")
		}
		count++
		if count > stageMaxEntries {
			return fail("filesystem entry quota exceeded")
		}
		if len(path) > 4096 || !utf8.ValidString(path) {
			return fail("pathname bound or encoding unsupported")
		}
		if path != "." && stageOmit(path) {
			omitted = append(omitted, path)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := root.Lstat(path)
		if err != nil {
			return fail("entry changed during enumeration")
		}
		record := map[string]any{"path": path, "mode": int(info.Mode().Perm()), "size": int64(0), "type": "directory", "digest": nil}
		switch {
		case info.IsDir():
			if destination != "" && path != "." {
				if err = os.Mkdir(filepath.Join(destination, path), 0700); err != nil {
					return fail("directory publication failed")
				}
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, err := root.Readlink(path)
			if err != nil {
				return fail("symlink changed")
			}
			resolved, err := filepath.EvalSymlinks(filepath.Join(source, path))
			if err != nil {
				return fail("symlink target unavailable")
			}
			rel, err := filepath.Rel(source, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fail("symlink outside capture")
			}
			if filepath.IsAbs(target) {
				return fail("absolute symlink cannot retain captured execution binding")
			}
			sum := sha256.Sum256([]byte(target))
			record["type"] = "symlink"
			record["size"] = int64(len(target))
			record["digest"] = "sha256:" + hex.EncodeToString(sum[:])
			if destination != "" {
				if err = os.Symlink(target, filepath.Join(destination, path)); err != nil {
					return fail("symlink publication failed")
				}
			}
		case info.Mode().IsRegular():
			if info.Size() > stageMaxFileBytes {
				return fail("per-file working-byte quota exceeded")
			}
			file, err := root.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
			if err != nil {
				return fail("regular input changed")
			}
			before, err := file.Stat()
			if err != nil || !os.SameFile(info, before) || !before.Mode().IsRegular() {
				file.Close()
				return fail("regular input identity changed")
			}
			var output *os.File
			var writer io.Writer
			hash := sha256.New()
			writer = hash
			if destination != "" {
				output, err = os.OpenFile(filepath.Join(destination, path), os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
				if err != nil {
					file.Close()
					return fail("regular output publication failed")
				}
				writer = io.MultiWriter(hash, output)
			}
			n, readErr := io.Copy(writer, io.LimitReader(file, stageMaxFileBytes+1))
			after, statErr := file.Stat()
			file.Close()
			if output != nil {
				if err = output.Chmod(info.Mode().Perm()); err == nil {
					err = output.Sync()
				}
				closeErr := output.Close()
				if err == nil {
					err = closeErr
				}
			}
			if readErr != nil || statErr != nil || err != nil || n > stageMaxFileBytes || n != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
				return fail("regular input changed or exceeded bound")
			}
			working += n
			if working > stageMaxWorkingBytes {
				return fail("total working-byte quota exceeded")
			}
			record["type"] = "file"
			record["size"] = n
			record["digest"] = "sha256:" + hex.EncodeToString(hash.Sum(nil))
		default:
			return fail("socket, device or special input unsupported")
		}
		records = append(records, record)
		return nil
	})
	if e != nil {
		return nil, nil, e
	}
	sort.Slice(records, func(i, j int) bool { return records[i]["path"].(string) < records[j]["path"].(string) })
	sort.Strings(omitted)
	// Apply directory modes last, after materialization, so read-only input
	// directories do not prevent copying their own children.
	if destination != "" {
		for i := len(records) - 1; i >= 0; i-- {
			r := records[i]
			if r["type"] == "directory" && r["path"] != "." {
				if e = os.Chmod(filepath.Join(destination, r["path"].(string)), fs.FileMode(r["mode"].(int))); e != nil {
					return nil, nil, fail("directory mode publication failed")
				}
			}
		}
	}
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return nil, nil, e
	}
	return records, omitted, e
}
