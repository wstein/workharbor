// SPDX-License-Identifier: EUPL-1.2

//go:build darwin

package ptytest

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	tcGet = syscall.TIOCGETA
	tcSet = syscall.TIOCSETA

	// Not exported by package syscall; values from <sys/ttycom.h>.
	tiocPTYGRANT = 0x20007454
	tiocPTYUNLK  = 0x20007452
	tiocPTYGNAME = 0x40807453
)

// prepareMaster grants and unlocks the slave side and returns its path.
func prepareMaster(master *os.File) (string, error) {
	if err := ioctl(master.Fd(), tiocPTYGRANT, 0); err != nil {
		return "", err
	}
	if err := ioctl(master.Fd(), tiocPTYUNLK, 0); err != nil {
		return "", err
	}
	var name [128]byte
	if err := ioctl(master.Fd(), tiocPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); err != nil {
		return "", err
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	return string(name[:n]), nil
}
