// SPDX-License-Identifier: EUPL-1.2
// Package storage owns durable project data independently of runtime instances.
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type Binding struct {
	OwnerID, HostID, WorkspaceID, StorageID, ReservationOperationID, ExistingHandle string
	Initial                                                                         config.Descriptor
	DataUID, DataGID                                                                uint32
}
type Handle struct {
	Parent, Data string
	Binding      Binding
}
type Store struct {
	Root       string
	checkpoint func(string) error
}

// Check is a trusted native current-operation/approval fence, never wire input.
// IDs and a configuration descriptor alone confer no filesystem effect authority.
type Check func(context.Context) error

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func failure(code string) error { return &config.Error{Code: code} }
func (b Binding) valid() bool {
	for _, id := range []string{b.OwnerID, b.HostID, b.WorkspaceID, b.StorageID, b.ReservationOperationID} {
		if !uuid.MatchString(id) {
			return false
		}
	}
	_, e := config.DecodeDescriptor(b.Initial.Map())
	return e == nil && b.DataUID != 4294967295 && b.DataGID != 4294967295
}
func marker(b Binding, complete bool) []byte {
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "owner_id": b.OwnerID, "host_id": b.HostID, "workspace_id": b.WorkspaceID, "storage_id": b.StorageID, "reservation_operation_id": b.ReservationOperationID, "initial_config": b.Initial.Map(), "data_uid": b.DataUID, "data_gid": b.DataGID, "complete": complete})
	return raw
}
func (s Store) point(name string) error {
	if s.checkpoint != nil {
		return s.checkpoint(name)
	}
	return nil
}
func check(ctx context.Context, authorize Check) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if authorize == nil {
		return failure("forbidden")
	}
	return authorize(ctx)
}

// Seed consumes a trusted immutable ORIGINAL source. Existing valid data is never
// copied, traversed, cleaned, chmodded or chowned, even for later config revisions.
func (s Store) Seed(ctx context.Context, b Binding, source *config.Resolved, authorize Check) (h Handle, err error) {
	if e := check(ctx, authorize); e != nil {
		return h, e
	}
	if !platformSupported() || !b.valid() || !filepath.IsAbs(s.Root) || filepath.Clean(s.Root) != s.Root {
		return h, failure("invalid_storage")
	}
	physical, e := filepath.EvalSymlinks(s.Root)
	if e != nil || physical != s.Root || !privatePath(s.Root, 0700, true) {
		return h, failure("invalid_storage")
	}
	final := filepath.Join(s.Root, b.StorageID)
	if b.ExistingHandle != "" && b.ExistingHandle != final {
		return h, failure("storage_conflict")
	}
	if !canChown(b.DataUID, b.DataGID) {
		return h, failure("unsupported_configuration")
	}
	lock, e := openLock(filepath.Join(s.Root, ".lock-"+b.StorageID))
	if e != nil {
		return h, failure("storage_failed")
	}
	defer lock.Close()
	for {
		e = tryLock(lock)
		if e == nil {
			break
		}
		if !lockBusy(e) {
			return h, failure("storage_failed")
		}
		select {
		case <-ctx.Done():
			return h, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() {
		if e := unlock(lock); e != nil && err == nil {
			h = Handle{}
			err = failure("storage_failed")
		}
	}()
	if e := check(ctx, authorize); e != nil {
		return h, e
	}
	if _, e := os.Lstat(final); e == nil {
		if !validFinal(final, b) {
			return h, failure("storage_conflict")
		}
		if syncDir(s.Root) != nil {
			return h, failure("storage_failed")
		}
		if e := check(ctx, authorize); e != nil {
			return h, e
		}
		return Handle{final, filepath.Join(final, "data"), b}, nil
	} else if !os.IsNotExist(e) {
		return h, failure("storage_failed")
	}
	if b.ExistingHandle != "" {
		return h, failure("storage_unavailable")
	}
	if source == nil || !reflect.DeepEqual(source.Descriptor.Map(), b.Initial.Map()) {
		return h, failure("seed_mismatch")
	}
	info, e := os.Lstat(source.ProjectsPath)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return h, failure("invalid_seed")
	}
	if e := s.discardOwnedTemps(b); e != nil {
		return h, e
	}
	temp, e := os.MkdirTemp(s.Root, ".seed-"+b.StorageID+"-")
	if e != nil {
		return h, failure("storage_failed")
	}
	published := false
	defer func() {
		if !published {
			if e := removeTemp(temp); e != nil && err == nil {
				h = Handle{}
				err = failure("storage_failed")
			}
		}
	}()
	if writePrivate(filepath.Join(temp, "owner.json"), marker(b, false)) != nil || syncDir(temp) != nil || syncDir(s.Root) != nil {
		return h, failure("storage_failed")
	}
	if e := s.point("owned_temp"); e != nil {
		return h, e
	}
	if e := copyData(ctx, source.ProjectsPath, filepath.Join(temp, "data"), b, s.point); e != nil {
		return h, e
	}
	if e := s.point("copy_complete"); e != nil {
		return h, e
	}
	if writePrivate(filepath.Join(temp, "seed.json"), marker(b, true)) != nil || syncDir(temp) != nil {
		return h, failure("storage_failed")
	}
	if e := s.point("manifest_synced"); e != nil {
		return h, e
	}
	if e := check(ctx, authorize); e != nil {
		return h, e
	}
	if e := s.point("before_rename"); e != nil {
		return h, e
	}
	if e := renameNoReplace(temp, final); e != nil {
		if !errors.Is(e, os.ErrExist) || !validFinal(final, b) {
			return h, failure("storage_conflict")
		}
	} else {
		published = true
	}
	if e := s.point("after_rename"); e != nil {
		return h, e
	}
	if syncDir(s.Root) != nil {
		return h, failure("storage_failed")
	}
	if e := check(ctx, authorize); e != nil {
		return h, e
	}
	return Handle{final, filepath.Join(final, "data"), b}, nil
}
func validFinal(parent string, b Binding) bool {
	if !privatePath(parent, 0700, true) || !privatePath(filepath.Join(parent, "seed.json"), 0600, false) {
		return false
	}
	raw, e := os.ReadFile(filepath.Join(parent, "seed.json"))
	if e != nil || !bytes.Equal(raw, marker(b, true)) {
		return false
	}
	info, e := os.Lstat(filepath.Join(parent, "data"))
	return e == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}
func (s Store) discardOwnedTemps(b Binding) error {
	entries, e := os.ReadDir(s.Root)
	if e != nil {
		return failure("storage_failed")
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".seed-"+b.StorageID+"-") {
			continue
		}
		parent := filepath.Join(s.Root, entry.Name())
		owner := filepath.Join(parent, "owner.json")
		if !privatePath(parent, 0700, true) || !privatePath(owner, 0600, false) {
			return failure("storage_conflict")
		}
		raw, e := os.ReadFile(owner)
		if e != nil || !bytes.Equal(raw, marker(b, false)) {
			return failure("storage_conflict")
		}
		if removeTemp(parent) != nil {
			return failure("storage_failed")
		}
	}
	return syncFixed(s.Root)
}
func syncFixed(path string) error {
	if syncDir(path) != nil {
		return failure("storage_failed")
	}
	return nil
}
func writePrivate(path string, raw []byte) error {
	file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = file.Write(raw)
	if e == nil {
		e = file.Sync()
	}
	ce := file.Close()
	if e == nil {
		e = ce
	}
	return e
}
func syncDir(path string) error {
	file, e := os.Open(path)
	if e != nil {
		return e
	}
	e = file.Sync()
	ce := file.Close()
	if e == nil {
		e = ce
	}
	return e
}
func copyData(ctx context.Context, source, dest string, b Binding, point func(string) error) error {
	physicalSource, evalErr := filepath.EvalSymlinks(source)
	if evalErr != nil {
		return failure("invalid_seed")
	}
	type directory struct {
		path string
		mode os.FileMode
	}
	dirs := []directory{}
	e := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return failure("invalid_seed")
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return failure("invalid_seed")
		}
		target := filepath.Join(dest, rel)
		info, e := entry.Info()
		if e != nil {
			return failure("invalid_seed")
		}
		mode := info.Mode().Perm() & 0777
		switch {
		case entry.IsDir():
			if os.Mkdir(target, 0700) != nil {
				return failure("storage_failed")
			}
			dirs = append(dirs, directory{target, mode})
		case info.Mode().IsRegular():
			in, e := openSource(path)
			if e != nil {
				return failure("invalid_seed")
			}
			out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				in.Close()
				return failure("storage_failed")
			}
			_, e = io.Copy(out, in)
			ice := in.Close()
			if e == nil {
				e = ice
			}
			if e == nil {
				e = out.Chown(int(b.DataUID), int(b.DataGID))
			}
			if e == nil {
				e = out.Chmod(mode)
			}
			if e == nil {
				e = out.Sync()
			}
			ce := out.Close()
			if e != nil || ce != nil {
				return failure("storage_failed")
			}
			if e := point("copy_file"); e != nil {
				return e
			}
		case info.Mode()&os.ModeSymlink != 0:
			link, e := os.Readlink(path)
			if e != nil || filepath.IsAbs(link) {
				return failure("invalid_seed")
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(rel), link))
			if resolved == ".." || strings.HasPrefix(resolved, "../") {
				return failure("invalid_seed")
			}
			// Clean alone cannot resolve "same/.." when same is itself a link.
			// Initial links must resolve completely inside the ORIGINAL copied
			// project subtree. Dangling and cyclic inputs are deliberately refused.
			actual, resolveErr := filepath.EvalSymlinks(path)
			actualRelative, relativeErr := filepath.Rel(physicalSource, actual)
			if resolveErr != nil || relativeErr != nil || !filepath.IsLocal(actualRelative) {
				return failure("invalid_seed")
			}
			if os.Symlink(link, target) != nil || os.Lchown(target, int(b.DataUID), int(b.DataGID)) != nil {
				return failure("storage_failed")
			}
		default:
			return failure("invalid_seed")
		}
		return nil
	})
	if e != nil {
		return e
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		// Keep an open directory descriptor before changing its owner/mode.
		f, e := os.Open(dirs[i].path)
		if e != nil {
			return failure("storage_failed")
		}
		e = f.Chown(int(b.DataUID), int(b.DataGID))
		if e == nil {
			e = f.Chmod(dirs[i].mode)
		}
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil || ce != nil {
			return failure("storage_failed")
		}
	}

	return nil
}

// CheckInitialOwner is a pre-effect capability predicate; it mutates no files.
func CheckInitialOwner(uid, gid uint32) error {
	if !platformSupported() || uid == 4294967295 || gid == 4294967295 || !canChown(uid, gid) {
		return failure("unsupported_configuration")
	}
	return nil
}

// removeTemp is called only for this invocation's generated parent, or an exact
// privately owned incomplete marker validated under the storage lock. Never final.
func removeTemp(parent string) error {
	var writable func(string) error
	writable = func(path string) error {
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if os.Chown(path, os.Geteuid(), os.Getegid()) != nil || os.Chmod(path, 0700) != nil {
			return os.ErrPermission
		}
		entries, e := os.ReadDir(path)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if e := writable(filepath.Join(path, entry.Name())); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e := writable(parent); e != nil {
		return e
	}
	return os.RemoveAll(parent)
}
