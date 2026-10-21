// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"context"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// DoctorHandler runs the local checks of `whr doctor` (FR-060) on ports. It
// wires the same read-only checker used by validate unless a validator port
// was supplied by the caller. Doctor supplies its remaining shared budget in ctx.
func DoctorHandler(p doctor.Ports) Handler {
	if p.Validator == nil {
		p.Validator = func(ctx context.Context, dir string) (doctor.Outcome, error) {
			report, err := workspacecfg.Validate(ctx, dir, workspacecfg.Options{})
			if err != nil {
				return doctor.Outcome{}, err
			}
			return doctor.Outcome{Errors: report.Counts.Error, Warnings: report.Counts.Warning}, nil
		}
	}
	return func(ctx context.Context, call Call) (Result, error) {
		if len(call.Args) > 0 {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "doctor takes no arguments")
		}
		rep := doctor.Run(ctx, p)
		// An interrupt or deadline during the run is exit 72 or 71, not a
		// verdict computed from half-finished measurements (FR-063).
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		res := Result{Value: rep, Human: rep.Human()}
		if rep.Counts.Fail == 0 {
			return res, nil
		}
		var ids []string
		for _, c := range rep.Checks {
			if c.Status == doctor.StatusFail {
				ids = append(ids, c.ID)
			}
		}
		// The report is still data: it goes to stdout (human) or the result (JSON).
		err := exitcode.Errorf(exitcode.PathConflict, "doctor found %d failed check(s): %s", len(ids), strings.Join(ids, ", "))
		err.Remediation = "Each failed check lists its remediation in the report."
		err.Result = rep
		return res, err
	}
}
