// SPDX-License-Identifier: EUPL-1.2

//go:build linux || darwin || freebsd

package doctor

import (
	"errors"
	"os"
	"syscall"
)

// freeBytes is the space available to an unprivileged user (statfs). A
// negative count (possible on FreeBSD) is not a measurement.
func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	if int64(st.Bavail) < 0 || int64(st.Bsize) <= 0 {
		return 0, errors.New("filesystem reports no usable free count")
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil //nolint:unconvert // field types differ per platform
}

// openNoFollow opens read-only without following a final symbolic link and
// without blocking on a FIFO.
func openNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}
