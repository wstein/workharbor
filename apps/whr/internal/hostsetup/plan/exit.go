//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"errors"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// RefusalCodes returns an independent copy of the Feature009 reason mapping.
func RefusalCodes() map[string]exitcode.Code {
	return map[string]exitcode.Code{
		"host_setup.platform_unsupported": 68, "host_setup.running_as_root": 68, "host_setup.run_locked": 68, "host_setup.profile_absent": 68, "host_setup.protocol_unwritable": 68,
		"host_setup.confirmation_missing": 70, "host_setup.sudo_requires_terminal": 70,
		"host_setup.unknown_item": 64, "host_setup.flag_not_valid_here": 64, "host_setup.catalogue_invalid": 74,
		"host_setup.plan_changed": 68, "host_setup.package_manager_missing": 68, "host_setup.package_manager_failed": 68, "host_setup.version_below_minimum": 68, "host_setup.sudo_failed": 68,
		"host_setup.step_timeout": 71, "host_setup.dependency_failed": 68, "host_setup.target_unsafe": 68, "host_setup.changed_since_preview": 68, "host_setup.backup_failed": 68, "host_setup.git_move_refused": 68, "host_setup.opted_out": 0, "host_setup.declined": 0,
	}
}

// ResultExit preserves the engine's authoritative decision; it does not derive
// mutation from receipts (applied, rolled back and Changed are distinct).
func ResultExit(r ApplyResult) (exitcode.Code, error) {
	switch exitcode.Code(r.ExitCode) {
	case exitcode.OK, exitcode.Usage, exitcode.Conflict, exitcode.NeedsHuman, exitcode.Timeout, exitcode.Interrupted, exitcode.PartialFailure, exitcode.Internal:
		return exitcode.Code(r.ExitCode), nil
	}
	return exitcode.Internal, errors.New("invalid host setup engine exit")
}

// ConfirmationHint contains only fixed flag names, never input or host data.
func ConfirmationHint() string {
	return "Confirm create-class work with --yes; confirm each modify-class item with --confirm <id>."
}
