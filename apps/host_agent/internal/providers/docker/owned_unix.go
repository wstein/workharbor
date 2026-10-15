//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"os"
	"path/filepath"
	"syscall"
)

func ownedDirectory(path string) bool {
	physical, e := filepath.EvalSymlinks(path)
	info, ie := os.Lstat(path)
	if e != nil || ie != nil || physical != path || !info.IsDir() || info.Mode().Perm() != 0700 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func ownedSocket(path string) bool {
	if !ownedDirectory(filepath.Dir(path)) {
		return false
	}
	info, e := os.Lstat(path)
	if e != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
}

func ownedRegular(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
}

func nativeDockerSocket(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	actual, e := filepath.EvalSymlinks(path)
	info, se := os.Lstat(path)
	if e != nil || se != nil || actual != path || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0007 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == 0 || stat.Uid == uint32(os.Geteuid())) && stat.Nlink == 1
}
