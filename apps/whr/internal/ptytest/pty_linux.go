// SPDX-License-Identifier: EUPL-1.2

//go:build linux

package ptytest

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	tcGet = syscall.TCGETS
	tcSet = syscall.TCSETS
)

// prepareMaster unlocks the slave side and returns its path.
func prepareMaster(master *os.File) (string, error) {
	var unlock int32
	if err := ioctl(master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		return "", err
	}
	var n uint32
	if err := ioctl(master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		return "", err
	}
	return "/dev/pts/" + strconv.Itoa(int(n)), nil
}
