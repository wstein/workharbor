// SPDX-License-Identifier: EUPL-1.2

//go:build unix

package doctor

import (
	"os/exec"
	"syscall"
)

// isolate puts git in its own process group and, when the context ends, kills
// the whole group, so that a wrapper's children are not orphaned.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
