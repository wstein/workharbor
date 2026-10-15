// SPDX-License-Identifier: EUPL-1.2
package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// Inspect is read-only ORIGINAL owner admission, never seed completion evidence.
// Even found=true must pass Seed's existing-final fsync/current Check path before
// first provider-handle observation or receipt. No retained data is traversed.
func (s Store) Inspect(ctx context.Context, b Binding, authorize Check) (Handle, bool, error) {
	if e := check(ctx, authorize); e != nil {
		return Handle{}, false, e
	}
	if !platformSupported() || !b.valid() || !filepath.IsAbs(s.Root) || filepath.Clean(s.Root) != s.Root {
		return Handle{}, false, failure("invalid_storage")
	}
	physical, e := filepath.EvalSymlinks(s.Root)
	if e != nil || physical != s.Root || !privatePath(s.Root, 0700, true) {
		return Handle{}, false, failure("invalid_storage")
	}
	final := filepath.Join(s.Root, b.StorageID)
	if b.ExistingHandle != "" && b.ExistingHandle != final {
		return Handle{}, false, failure("storage_conflict")
	}
	if _, e = os.Lstat(final); os.IsNotExist(e) {
		if b.ExistingHandle != "" {
			return Handle{}, false, failure("storage_unavailable")
		}
		if e = check(ctx, authorize); e != nil {
			return Handle{}, false, e
		}
		return Handle{}, false, nil
	} else if e != nil {
		return Handle{}, false, failure("storage_failed")
	}
	markerPath := filepath.Join(final, "seed.json")
	if !privatePath(final, 0700, true) || !privatePath(markerPath, 0600, false) {
		return Handle{}, false, failure("storage_conflict")
	}
	raw, e := os.ReadFile(markerPath)
	if e != nil {
		return Handle{}, false, failure("storage_conflict")
	}
	var owner struct {
		UID uint32 `json:"data_uid"`
		GID uint32 `json:"data_gid"`
	}
	if json.Unmarshal(raw, &owner) != nil || owner.UID == 4294967295 || owner.GID == 4294967295 {
		return Handle{}, false, failure("storage_conflict")
	}
	b.DataUID = owner.UID
	b.DataGID = owner.GID
	// Exact canonical marker equality rejects duplicate, extra, missing, aliased or
	// malformed fields and binds every ORIGINAL ID/descriptor plus numeric owner.
	if !validFinal(final, b) {
		return Handle{}, false, failure("storage_conflict")
	}
	if e = check(ctx, authorize); e != nil {
		return Handle{}, false, e
	}
	return Handle{Parent: final, Data: filepath.Join(final, "data"), Binding: b}, true, nil
}
