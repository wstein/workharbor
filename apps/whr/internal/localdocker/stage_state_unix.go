// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

// stageStore holds a daemon-scoped cooperative lock. It never steals retained
// attempts or interprets Docker labels as a substitute for the private journal.
type stageStore struct {
	path      string
	dir, lock *os.File
}
type stageRecord struct {
	Version      int    `json:"version"`
	OperationID  string `json:"operation_id"`
	Sequence     int    `json:"sequence"`
	PreviousHash string `json:"previous_hash"`
	Hash         string `json:"hash"`
	State        string `json:"state"`
	Fingerprint  string `json:"fingerprint"`
	Action       string `json:"action"`
	Observation  string `json:"observation"`
}

var stageStates = []string{"planned", "allocating", "candidate_resources", "candidate_hooks", "verifying", "staged_verified", "exporting_results", "importing_results", "promoting", "primary_resources", "primary_verified", "candidate_removed", "home_removed", "complete"}

func stageConflict(reason string) error {
	return exitcode.Errorf(exitcode.PathConflict, "local stage %s", reason)
}

func openStageStore(root, daemon, endpoint string) (*stageStore, error) {
	if !filepath.IsAbs(root) || daemon == "" || !strings.HasPrefix(endpoint, "unix:///") {
		return nil, stageConflict("state path is invalid")
	}
	// Open each component relative to the previous directory, never following a
	// symlink. Existing ancestors may be shared; the state root itself is private.
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, stageConflict("state path unavailable")
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(root), "/"), "/")
	for i, p := range parts {
		next, err := unix.Openat(fd, p, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.ENOENT) {
			if err = unix.Mkdirat(fd, p, 0700); err == nil {
				if err = unix.Fsync(fd); err != nil {
					unix.Close(fd)
					return nil, stageConflict("state directory durability uncertain")
				}
				next, err = unix.Openat(fd, p, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if err != nil {
			return nil, stageConflict("state path unavailable or symlinked")
		}
		fd = next
		if i == len(parts)-1 {
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0700 {
				unix.Close(fd)
				return nil, stageConflict("state path ownership or mode")
			}
		}
	}
	sum := sha256.Sum256([]byte(daemon + "\x00" + endpoint))
	key := hex.EncodeToString(sum[:])
	if e = unix.Mkdirat(fd, key, 0700); e != nil && !errors.Is(e, unix.EEXIST) {
		unix.Close(fd)
		return nil, stageConflict("state path unavailable")
	}
	if e = unix.Fsync(fd); e != nil {
		unix.Close(fd)
		return nil, stageConflict("daemon directory durability uncertain")
	}
	child, e := unix.Openat(fd, key, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	unix.Close(fd)
	if e != nil {
		return nil, stageConflict("state path unavailable")
	}
	var st unix.Stat_t
	if unix.Fstat(child, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0700 {
		unix.Close(child)
		return nil, stageConflict("state path ownership or mode")
	}
	s := &stageStore{path: filepath.Join(root, key), dir: os.NewFile(uintptr(child), key)}
	lf, e := unix.Openat(child, "lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		s.Close()
		return nil, stageConflict("operation lock unavailable")
	}
	s.lock = os.NewFile(uintptr(lf), "lock")
	if e = unix.Fstat(lf, &st); e != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0600 || st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG {
		s.Close()
		return nil, stageConflict("operation lock ownership or mode")
	}
	if e = unix.Flock(lf, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		s.Close()
		return nil, stageConflict("operation lock is held")
	}
	return s, nil
}
func (s *stageStore) Close() {
	if s.lock != nil {
		s.lock.Close()
		s.lock = nil
	}
	if s.dir != nil {
		s.dir.Close()
		s.dir = nil
	}
}
func recordHash(r stageRecord) string {
	r.Hash = ""
	b, _ := json.Marshal(r)
	b = append(b, '\n')
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func (s *stageStore) Records() ([]stageRecord, error) {
	fd, e := unix.Openat(int(s.dir.Fd()), "journal.json", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(e, unix.ENOENT) {
		return nil, nil
	}
	if e != nil {
		return nil, stageConflict("journal integrity: unreadable")
	}
	f := os.NewFile(uintptr(fd), "journal")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0600 || st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size > 4<<20 {
		return nil, stageConflict("journal integrity: ownership, mode or size")
	}
	b, e := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if e != nil || len(b) > 4<<20 {
		return nil, stageConflict("journal integrity: read bound")
	}
	return decodeStageJournal(b)

}
func (s *stageStore) Begin(op, digest string) error {
	if !lowerHex32(op) || len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return stageConflict("intent identity is invalid")
	}
	rows, e := s.Records()
	if e != nil {
		return e
	}
	if len(rows) != 0 {
		if rows[len(rows)-1].State != "complete" || rows[len(rows)-1].Observation != "observed" {
			return stageConflict("retained attempt requires owner reconciliation")
		}
		if e = s.archiveComplete(rows); e != nil {
			return e
		}
	}
	fd, e := unix.Openat(int(s.dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return stageConflict("retained attempt inventory unavailable")
	}
	directory := os.NewFile(uintptr(fd), "operation inventory")
	entries, e := directory.ReadDir(stageMaxEntries + 1)
	directory.Close()
	if e != nil && e != io.EOF {
		return stageConflict("retained attempt inventory unavailable")
	}
	if len(entries) > stageMaxEntries {
		return stageConflict("retained attempt inventory quota")
	}
	for _, entry := range entries {
		if entry.Name() != "lock" {
			if !lowerHex32(entry.Name()) || !entry.IsDir() || s.checkCompletedArchive(entry.Name()) != nil {
				return stageConflict("retained attempt requires owner reconciliation")
			}
		}
	}
	if e = unix.Mkdirat(int(s.dir.Fd()), op, 0700); e != nil {
		return stageConflict("retained attempt or operation identity collision")
	}
	if e = s.dir.Sync(); e != nil {
		return stageConflict("operation directory durability uncertain")
	}
	return s.write([]stageRecord{{Version: 1, OperationID: op, Sequence: 1, State: "planned", Fingerprint: digest, Observation: "observed"}}, true)
}
func (s *stageStore) Advance(state, action, observation string) error {
	rows, e := s.Records()
	if e != nil {
		return e
	}
	if len(rows) == 0 {
		return stageConflict("state transition without intent")
	}
	last := rows[len(rows)-1]
	if last.Observation == "attempted" && (state != last.State || action != last.Action || observation == "attempted") {
		return stageConflict("state transition after unobserved effect")
	}
	valid := state == last.State
	for i, v := range stageStates {
		if v == last.State && i+1 < len(stageStates) && stageStates[i+1] == state {
			valid = true
		}
	}
	if last.State == "verifying" && state == "staged_unverified" {
		valid = true
	}
	if !valid || last.State == "staged_unverified" || last.State == "complete" || last.Observation == "uncertain" {
		return stageConflict("state transition requires owner reconciliation")
	}
	if observation != "attempted" && observation != "observed" && observation != "uncertain" {
		return stageConflict("state transition observation")
	}
	if len(rows) >= stageMaxJournalRecords {
		return stageConflict("journal record quota")
	}
	rows = append(rows, stageRecord{Version: 1, OperationID: last.OperationID, Sequence: last.Sequence + 1, PreviousHash: last.Hash, State: state, Fingerprint: last.Fingerprint, Action: action, Observation: observation})
	return s.write(rows, false)
}
func (s *stageStore) write(rows []stageRecord, exclusive bool) error {
	for i := range rows {
		rows[i].Hash = recordHash(rows[i])
	}
	b, e := json.Marshal(rows)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	if len(b) > 4<<20 {
		return stageConflict("journal size quota")
	}
	// Exclusive temporary names and rename publish a complete, synced journal.
	name := fmt.Sprintf(".journal-%d-%d", os.Getpid(), len(rows))
	fd, e := unix.Openat(int(s.dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return stageConflict("journal publication unavailable")
	}
	f := os.NewFile(uintptr(fd), name)
	defer unix.Unlinkat(int(s.dir.Fd()), name, 0)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return stageConflict("journal publication failed")
	}
	if exclusive {
		e = unix.Linkat(int(s.dir.Fd()), name, int(s.dir.Fd()), "journal.json", 0)
	} else {
		e = unix.Renameat(int(s.dir.Fd()), name, int(s.dir.Fd()), "journal.json")
	}
	if e != nil {
		return stageConflict("journal publication failed")
	}
	if e = s.dir.Sync(); e != nil {
		return stageConflict("journal durability uncertain")
	}
	return nil
}
