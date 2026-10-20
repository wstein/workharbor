//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"golang.org/x/sys/unix"
	"io/fs"
	"os"
)

func createExclusive(root *os.Root, name string, mode fs.FileMode) (*os.File, error) {
	return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW|unix.O_NONBLOCK, mode)
}
