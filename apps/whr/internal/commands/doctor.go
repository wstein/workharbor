// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"context"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// DoctorHandler runs the local checks of `whr doctor` (FR-060) on ports. It
// is the only place that wires doctor's validator port: until T034 connects
// the real checker, config.valid reports skipped with not_in_increment.
func DoctorHandler(p doctor.Ports) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		if len(call.Args) > 0 {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "doctor takes no arguments")
		}
		rep := doctor.Run(ctx, p)
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
