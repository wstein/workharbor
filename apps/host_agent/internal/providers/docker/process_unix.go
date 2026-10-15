//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func ownedProcessGroup(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		e := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
	return nil
}
