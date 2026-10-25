// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"crypto/sha256"
	"encoding/hex"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

// Promotion locks only existing state. Missing intent is never repaired from
// labels, even during preview; this function performs no persistent writes.
func openPromotionStore(root, daemon, endpoint string) (*stageStore, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || daemon == "" || !strings.HasPrefix(endpoint, "unix:///") {
		return nil, stageConflict("promotion state path is invalid")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, stageConflict("promotion state is absent or symlinked")
	}
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, stageConflict("promotion state is absent or symlinked")
		}
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0700 {
		unix.Close(fd)
		return nil, stageConflict("promotion state ownership or mode")
	}
	sum := sha256.Sum256([]byte(daemon))
	key := hex.EncodeToString(sum[:])
	child, err := unix.Openat(fd, key, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	unix.Close(fd)
	if err != nil {
		return nil, stageConflict("promotion state is absent or symlinked")
	}
	if unix.Fstat(child, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0700 {
		unix.Close(child)
		return nil, stageConflict("promotion state ownership or mode")
	}
	s := &stageStore{promotion: true, path: filepath.Join(root, key), dir: os.NewFile(uintptr(child), key)}
	lock, err := unix.Openat(child, "lock", unix.O_RDWR|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		s.Close()
		return nil, stageConflict("promotion lock is unavailable or symlinked")
	}
	s.lock = os.NewFile(uintptr(lock), "lock")
	if unix.Fstat(lock, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0600 || st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG {
		s.Close()
		return nil, stageConflict("promotion lock ownership or mode")
	}
	if unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB) != nil {
		s.Close()
		return nil, stageConflict("promotion operation lock is held")
	}
	return s, nil
}
