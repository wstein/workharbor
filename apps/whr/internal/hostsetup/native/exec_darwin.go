// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package native

import "os"

// macOS cannot exec /dev/fd/N. Execute the verified on-disk path instead,
// after checking immediately before exec that it is still the opened file.
func (h *Host) executeRoute(f *os.File, virtual string) (string, error) {
	return h.verifiedDiskPath(f, virtual)
}

// exitedLeaderSignalEPERM is true: macOS answers EPERM when signalling the
// process group of an exited, unreaped (zombie) leader.
const exitedLeaderSignalEPERM = true
