// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package storage

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func platformSupported() bool { return true }
func privatePath(path string, mode os.FileMode, dir bool) bool {
	info, e := os.Lstat(path)
	if e != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid()) && info.Mode().Perm() == mode && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 && info.IsDir() == dir && info.Mode()&os.ModeSymlink == 0 && (dir || (info.Mode().IsRegular() && st.Nlink == 1))
}
func openSource(path string) (*os.File, error) {
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), path), nil
}
func openLock(path string) (*os.File, error) {
	fd, e := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	file := os.NewFile(uintptr(fd), path)
	info, e := file.Stat()
	if e != nil {
		file.Close()
		return nil, e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		file.Close()
		return nil, os.ErrPermission
	}
	return file, nil
}
func tryLock(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func unlock(file *os.File) error  { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }
func lockBusy(e error) bool       { return e == unix.EWOULDBLOCK || e == unix.EAGAIN }
