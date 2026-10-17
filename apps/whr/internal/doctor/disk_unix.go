// SPDX-License-Identifier: EUPL-1.2

//go:build unix

package doctor

import "syscall"

// freeBytes is the space available to an unprivileged user (statfs).
func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil //nolint:unconvert // field types differ per platform
}
