// SPDX-License-Identifier: EUPL-1.2
//go:build !unix

package localdocker

import "os/exec"

func isolate(cmd *exec.Cmd) func() {
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}
