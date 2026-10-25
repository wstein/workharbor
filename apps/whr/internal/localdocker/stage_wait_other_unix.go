//go:build unix && !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package localdocker

func stageWaitLeader(int) error {
	return stageConflict("native wait-without-reap capability unavailable")
}
func stageWaitSupported() bool { return false }
