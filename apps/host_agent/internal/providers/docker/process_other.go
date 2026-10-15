//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package docker

import "os/exec"

func ownedProcessGroup(*exec.Cmd) error { return fail("provider_unavailable") }
