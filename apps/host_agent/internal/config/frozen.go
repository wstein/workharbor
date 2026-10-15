// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type frozenEntry struct {
	Path    string
	Mode    uint32
	Size    int64
	Content string
}

func fingerprint(ctx context.Context, root string) (string, error) {
	physical, e := filepath.EvalSymlinks(root)
	if e != nil || physical != root {
		return "", invalidConfiguration()
	}
	h := sha256.New()
	enc := json.NewEncoder(h)
	e = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if walkErr != nil {
			return walkErr
		}
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		relative, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		record := frozenEntry{Path: filepath.ToSlash(relative), Mode: uint32(info.Mode()), Size: info.Size()}
		switch {
		case info.Mode().IsRegular():
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			actual, e := f.Stat()
			if e != nil || !os.SameFile(info, actual) {
				f.Close()
				return invalidConfiguration()
			}
			digest := sha256.New()
			_, e = io.Copy(digest, f)
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
			record.Content = hex.EncodeToString(digest.Sum(nil))
		case info.Mode()&os.ModeSymlink != 0:
			record.Content, e = os.Readlink(path)
			if e != nil {
				return e
			}
		case info.IsDir():
			record.Size = 0
		default:
			return unsupportedConfiguration()
		}
		return enc.Encode(record)
	})
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Check verifies resolver-owned immutable configuration/build/initial-copy inputs.
// It never inspects retained project data or grants operation/approval authority.
func materializedFields(s *Resolved) []byte {
	raw, _ := json.Marshal(map[string]any{"root": s.Root, "devcontainer": s.DevcontainerPath, "projects": s.ProjectsPath, "descriptor": s.Descriptor.Map(), "source": s.Source, "bases": s.Bases})
	return raw
}
func (s *Resolved) Check() error { return s.CheckContext(context.Background()) }
func (s *Resolved) CheckContext(ctx context.Context) error {
	if s == nil || s.frozen == "" || s.privateParent == "" || len(s.sealed) == 0 || !bytes.Equal(s.sealed, materializedFields(s)) {
		return &Error{Code: "configuration_changed"}
	}
	actual, e := fingerprint(ctx, s.Root)
	if e != nil || actual != s.frozen {
		return &Error{Code: "configuration_changed"}
	}
	return nil
}
