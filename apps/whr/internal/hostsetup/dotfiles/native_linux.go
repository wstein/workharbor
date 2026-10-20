//go:build linux

// SPDX-License-Identifier: EUPL-1.2
package dotfiles

import "golang.org/x/sys/unix"

func renameAbsent(a int, source string, b int, target string) error {
	return unix.Renameat2(a, source, b, target, unix.RENAME_NOREPLACE)
}
