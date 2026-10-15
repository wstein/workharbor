// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package storage

import (
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"strings"
)

func renameNoReplace(old, new string) error {
	return unix.Renameat2(unix.AT_FDCWD, old, unix.AT_FDCWD, new, unix.RENAME_NOREPLACE)
}
func canChown(uid, gid uint32) bool {
	if uint32(os.Geteuid()) == uid && uint32(os.Getegid()) == gid {
		return true
	}
	if os.Geteuid() != 0 {
		return false
	}
	raw, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			v, e := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "CapEff:")), 16, 64)
			return e == nil && v&1 == 1
		}
	}
	return false
}
