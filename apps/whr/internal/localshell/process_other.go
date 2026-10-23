// SPDX-License-Identifier: EUPL-1.2
//go:build !unix

package localshell

import "os/exec"

func superviseInteractive(*exec.Cmd) {}
func interactiveStatus(cmd *exec.Cmd) int {
	status := cmd.ProcessState.ExitCode()
	if status < 0 {
		return 255
	}
	return status
}
