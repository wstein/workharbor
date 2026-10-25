// SPDX-License-Identifier: EUPL-1.2
package localdocker

import "golang.org/x/sys/unix"

func stageWaitLeader(pid int) error {
	var info unix.Siginfo
	for {
		e := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if e == unix.EINTR {
			continue
		}
		return e
	}
}
func stageWaitSupported() bool { return true }
