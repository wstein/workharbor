//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"reflect"
	"time"

	"golang.org/x/sys/unix"
)

const maxProfileBytes = 64 * 1024

// HostProfile contains selection metadata only, never host paths or Git values.
// There is one current schema; unknown fields/versions are rejected.
type HostProfile struct {
	SchemaVersion    int               `json:"schema_version"`
	CatalogueVersion string            `json:"catalogue_version"`
	Updated          string            `json:"updated"`
	Items            map[string]string `json:"items"`
}

func validProfile(p HostProfile) bool {
	if p.SchemaVersion != 1 || !versionPattern.MatchString(p.CatalogueVersion) || p.Items == nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, p.Updated)
	if err != nil || t.IsZero() || p.Updated[len(p.Updated)-1] != 'Z' {
		return false
	}
	for id, value := range p.Items {
		if !items[id] || value != "selected" && value != "opted_out" {
			return false
		}
	}
	return true
}
func decodeProfile(b []byte) (HostProfile, error) {
	var p HostProfile
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !validProfile(p) {
		return HostProfile{}, ErrUnsafe
	}
	return p, nil
}
func readProfile(root int) (HostProfile, error) {
	dir, err := directory(root, ".config/whr", false, true)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return HostProfile{}, fs.ErrNotExist
		}
		return HostProfile{}, ErrUnsafe
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, "host-profile.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return HostProfile{}, fs.ErrNotExist
	}
	if err != nil {
		return HostProfile{}, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), "host-profile")
	defer f.Close()
	if !regular(fd, 0600) {
		return HostProfile{}, ErrUnsafe
	}
	b, err := io.ReadAll(io.LimitReader(f, maxProfileBytes+1))
	if err != nil || len(b) > maxProfileBytes {
		return HostProfile{}, ErrUnsafe
	}
	return decodeProfile(b)
}
func (s *Store) ReadProfile() (HostProfile, error) {
	root, err := s.duplicate()
	if err != nil {
		return HostProfile{}, err
	}
	defer unix.Close(root)
	return readProfile(root)
}

// StoreProfile requires the real run's lock and healthy durable protocol. Equal
// effective selections retain the existing bytes and timestamp on a rerun.
func (r *Run) StoreProfile(p HostProfile) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.finished || r.poisoned || r.lock == nil || !validProfile(p) {
		return false, ErrUnsafe
	}
	// Verify both durable handles immediately before a managed write. A prior
	// append failure permanently poisons the run; a lost handle does so here.
	if r.json == nil || r.text == nil || r.json.Sync() != nil || r.text.Sync() != nil {
		r.poisoned = true
		return false, ErrProtocol
	}
	old, err := readProfile(r.root)
	if err == nil && old.CatalogueVersion == p.CatalogueVersion && reflect.DeepEqual(old.Items, p.Items) {
		return false, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, ErrUnsafe
	}
	dir, err := directory(r.root, ".config/whr", true, true)
	if err != nil {
		return false, ErrUnsafe
	}
	defer unix.Close(dir)
	b, err := json.Marshal(p)
	if err != nil || len(b) > maxProfileBytes {
		return false, ErrUnsafe
	}
	staged := ".host-profile-" + r.id
	f, err := exclusive(dir, staged)
	if err != nil {
		return false, ErrUnsafe
	}
	// A staging file is owned exclusively by this call. No protocol/backup is
	// removed. Keep a failed stage for diagnosis rather than risk deleting a swap.
	if appendSync(f, append(b, '\n')) != nil {
		f.Close()
		return false, ErrUnsafe
	}
	if f.Close() != nil {
		return false, ErrUnsafe
	}
	var st unix.Stat_t
	if e := unix.Fstatat(dir, "host-profile.json", &st, unix.AT_SYMLINK_NOFOLLOW); e != nil && !errors.Is(e, unix.ENOENT) || e == nil && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Nlink != 1) {
		return false, ErrUnsafe
	}
	if unix.Renameat(dir, staged, dir, "host-profile.json") != nil || unix.Fsync(dir) != nil {
		return false, ErrUnsafe
	}
	return true, nil
}

// Selection returns fresh maps suitable for plan.Input. A remembered opt-out
// always overrides a default selection, including reserved git-config.
func (p HostProfile) Selection() (map[string]bool, map[string]bool) {
	selected, off := map[string]bool{}, map[string]bool{}
	for id, value := range p.Items {
		selected[id] = value == "selected"
		off[id] = value == "opted_out"
	}
	return selected, off
}
