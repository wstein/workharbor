// SPDX-License-Identifier: EUPL-1.2

package ptytest

// SetKiller replaces the process-group killer for one test and returns the
// function that restores it.
func SetKiller(f func(pid int)) (restore func()) {
	old := killer
	killer = f
	return func() { killer = old }
}
