// SPDX-License-Identifier: EUPL-1.2

//go:build !unix

package doctor

import "os/exec"

// isolate has no process groups to use here; the direct child is still killed.
func isolate(*exec.Cmd) {}
