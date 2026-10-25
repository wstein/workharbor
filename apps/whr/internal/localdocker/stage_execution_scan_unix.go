// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"golang.org/x/sys/unix"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Execution inventory deliberately visits only admitted dependency roots. An
// ordinary candidate result file cannot become a new executed-input dependency.
func scanStageFilesSelected(ctx context.Context, source, destination string, selected []string) ([]map[string]any, []string, error) {
	if destination != "" {
		return nil, nil, stageConflict("execution inventory cannot publish files")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, nil, stageConflict("execution inventory root unavailable")
	}
	defer root.Close()
	records := []map[string]any{}
	omitted := []string{}
	count := 0
	var bytesRead int64
	limits := stageCaptureLimitsFor(ctx)
	fail := func(reason string) error { return stageConflict("execution inventory " + reason) }
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return fail("enumeration unavailable")
		}
		if path != "." && !stageSelectedPath(path, selected) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		count++
		if count > limits.WorkingEntries {
			return fail("entry quota exceeded")
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
			return fail("entry unavailable")
		}
		record := map[string]any{"path": path, "mode": int(info.Mode().Perm()), "size": int64(0), "type": "directory", "digest": nil}
		switch {
		case info.IsDir():
		case info.Mode()&os.ModeSymlink != 0:
			target, err := root.Readlink(path)
			if err != nil {
				return fail("symlink changed")
			}
			physical, err := filepath.EvalSymlinks(filepath.Join(source, path))
			if err != nil {
				return fail("symlink target unavailable")
			}
			rel, err := filepath.Rel(source, physical)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(target) {
				return fail("symlink outside execution binding")
			}
			sum := sha256.Sum256([]byte(target))
			record["type"] = "symlink"
			record["size"] = int64(len(target))
			record["digest"] = "sha256:" + hex.EncodeToString(sum[:])
		case info.Mode().IsRegular():
			if info.Size() > limits.FileBytes {
				return fail("file quota exceeded")
			}
			file, err := root.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if err != nil {
				return fail("file unavailable")
			}
			before, err := file.Stat()
			if err != nil || !os.SameFile(info, before) || !before.Mode().IsRegular() {
				file.Close()
				return fail("file identity changed")
			}
			hash := sha256.New()
			n, readErr := io.Copy(hash, io.LimitReader(stageContextReader{ctx: ctx, Reader: file}, limits.FileBytes+1))
			after, statErr := file.Stat()
			file.Close()
			if readErr != nil || statErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() || n != before.Size() {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fail("file changed")
			}
			bytesRead += n
			if n > limits.FileBytes || bytesRead > limits.WorkingBytes {
				return fail("byte quota exceeded")
			}
			record["type"] = "file"
			record["size"] = n
			record["digest"] = "sha256:" + hex.EncodeToString(hash.Sum(nil))
		default:
			return fail("file type unsupported")
		}
		records = append(records, record)
		return nil
	})
	return records, omitted, err
}
func stageSelectedPath(path string, selected []string) bool {
	for _, root := range selected {
		if root == "." || path == root || strings.HasPrefix(path, root+string(filepath.Separator)) || strings.HasPrefix(root, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Candidate-specific export restrictions belong here, not to source capture.
func readStageCandidateGitSnapshot(ctx context.Context, source string) (stageGitSnapshot, error) {
	snapshot, err := readStageGitSnapshotContext(ctx, source)
	if err != nil {
		return snapshot, err
	}
	for name := range snapshot.Refs {
		if strings.HasPrefix(name, "refs/replace/") {
			return snapshot, stageConflict("Git replacement refs unsupported")
		}
		if stageExportRef(name) {
			data, err := stageAdminFile(snapshot.CommonDir, filepath.FromSlash(name), 4096)
			if err != nil && !os.IsNotExist(err) {
				return snapshot, err
			}
			if strings.HasPrefix(string(data), "ref: ") {
				return snapshot, stageConflict("Git symbolic export ref unsupported")
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(snapshot.ObjectDir, "info", "alternates")); !os.IsNotExist(err) {
		return snapshot, stageConflict("candidate Git alternates unsupported")
	}
	return snapshot, nil
}
