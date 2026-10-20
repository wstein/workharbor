// SPDX-License-Identifier: EUPL-1.2

//go:build unix

package workspacecfg

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// openLeafNoFollow opens one component relative to an already-held parent.
// Root.OpenFile deliberately resolves in-root links even with O_NOFOLLOW.
func openLeafNoFollow(parent *os.Root, leaf string) (*os.File, error) {
	if leaf == "" || leaf == "." || leaf == ".." || strings.Contains(leaf, "/") {
		return nil, os.ErrPermission
	}
	directory, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	raw, err := directory.SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	var openErr error
	controlErr := raw.Control(func(parentFD uintptr) {
		for {
			fd, openErr = unix.Openat(int(parentFD), leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if openErr != unix.EINTR {
				break
			}
		}
	})
	// Until ownership transfers to os.File, every failure closes only a valid FD.
	owned := false
	defer func() {
		if !owned && fd >= 0 {
			_ = unix.Close(fd)
		}
	}()
	if controlErr != nil {
		return nil, controlErr
	}
	if openErr != nil {
		return nil, openErr
	}
	// NewFile otherwise treats O_NONBLOCK as pollable, including regular files
	// on Darwin/BSD. readFile checks the opened type before performing any read.
	if err := unix.SetNonblock(fd, false); err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), leaf)
	if f == nil {
		return nil, os.ErrInvalid
	}
	owned = true
	return f, nil
}
