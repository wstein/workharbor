// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package native

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

// Darwin waitid has historically reported stopped children despite WEXITED.
// Query only this owned PID instead; sys/proc.h's SZOMB=5 means exited, unreaped.
// This metadata lookup never releases/reaps the process or prints host evidence.
func childExited(pid int) (bool, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, unix.ESRCH) {
		return false, errOwnershipLost
	}
	if err != nil {
		return false, err
	}
	if info.Proc.P_pid != int32(pid) || info.Eproc.Ppid != int32(os.Getpid()) {
		return false, errOwnershipLost
	}
	const szomb = 5
	return info.Proc.P_stat == szomb, nil
}
