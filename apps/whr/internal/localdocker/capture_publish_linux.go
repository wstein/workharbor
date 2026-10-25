// SPDX-License-Identifier: EUPL-1.2
package localdocker

import "golang.org/x/sys/unix"

func publishStageClone(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}
