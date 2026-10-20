//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"golang.org/x/sys/unix"
	"os"
)

func openObservedFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
}
