// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package native

import "os"

// Linux executes the opened descriptor, pinning the file across pathname replacement.
func (h *Host) executeRoute(f *os.File, virtual string) (string, error) {
	return "/dev/fd/3", nil
}

// exitedLeaderSignalEPERM is false: Linux signals zombie leaders' groups without error.
const exitedLeaderSignalEPERM = false
