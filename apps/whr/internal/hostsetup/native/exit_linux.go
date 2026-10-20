// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package native

import (
	"errors"
	"golang.org/x/sys/unix"
)

// WNOWAIT observes exit while reserving the owned child PID for final cleanup.
func childExited(pid int) (bool, error) {
	var info unix.Siginfo
	err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT|unix.WNOHANG, nil)
	if errors.Is(err, unix.ECHILD) {
		return false, errOwnershipLost
	}
	if errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return info.Signo != 0, err
}
