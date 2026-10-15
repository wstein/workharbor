// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package storage

import (
	"golang.org/x/sys/unix"
	"os"
)

func renameNoReplace(old, new string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, old, unix.AT_FDCWD, new, unix.RENAME_EXCL)
}
func canChown(uid, gid uint32) bool {
	return os.Geteuid() == 0 || (uint32(os.Geteuid()) == uid && uint32(os.Getegid()) == gid)
}
