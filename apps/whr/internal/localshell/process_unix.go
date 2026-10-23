// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localshell

import (
	"os/exec"
	"syscall"
)

func superviseInteractive(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
}
func interactiveStatus(cmd *exec.Cmd) int {
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return cmd.ProcessState.ExitCode()
}
