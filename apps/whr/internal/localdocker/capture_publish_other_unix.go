// SPDX-License-Identifier: EUPL-1.2
//go:build unix && !linux && !darwin

package localdocker

func publishStageClone(from, to string) error {
	return stageConflict("atomic no-replace clone publication unavailable")
}
