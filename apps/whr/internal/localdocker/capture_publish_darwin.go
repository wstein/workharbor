// SPDX-License-Identifier: EUPL-1.2
package localdocker

import "golang.org/x/sys/unix"

func publishStageClone(from, to string) error {
	return unix.RenamexNp(from, to, unix.RENAME_EXCL)
}
