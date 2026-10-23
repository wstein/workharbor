// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"os/exec"
	"syscall"
)

// Cleanup also runs after Wait returns: an exited wrapper can leave descendants
// holding its pipes after CommandContext's process watcher has already stopped.
func isolate(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	killGroup := func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.Cancel = killGroup
	return func() { _ = killGroup() }
}
