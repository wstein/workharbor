// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"golang.org/x/sys/unix"
	"syscall"
	"unsafe"
)

// Darwin exposes waitid(2) but x/sys does not wrap it. The fixed native ABI uses
// idtype P_PID=1 and a caller-owned siginfo buffer. WNOWAIT retains the zombie
// leader until exec.Cmd.Wait, after all process-group signaling has stopped.
func stageWaitLeader(pid int) error {
	var info [256]byte
	for {
		_, _, e := syscall.Syscall6(unix.SYS_WAITID, 1, uintptr(pid), uintptr(unsafe.Pointer(&info[0])), unix.WEXITED|unix.WNOWAIT, 0, 0)
		if e == syscall.EINTR {
			continue
		}
		if e != 0 {
			return e
		}
		return nil
	}
}
func stageWaitSupported() bool { return true }
