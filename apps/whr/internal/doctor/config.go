// SPDX-License-Identifier: EUPL-1.2

package doctor

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
)

// config reports config.valid: the validate result for the current directory
// (never a parent) through the validator port, on the remainder of the budget.
func (r *run) config() Check {
	const id = "config.valid"
	if r.cwd == "" {
		return unknown(id, reasonNotMeasurable, "The current directory is unknown.", "Run whr doctor from an existing directory.", nil)
	}
	if _, err := r.p.Lstat(filepath.Join(r.cwd, "workharbor.json")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Check{ID: id, Status: StatusSkipped, Reason: reasonPrecondition,
				Message:     "There is no workharbor.json in the current directory.",
				Remediation: "Run whr doctor in a workspace directory, or whr init to create one."}
		}
		return unknown(id, reasonNotMeasurable, "workharbor.json could not be inspected.", "Check the permissions of the current directory.", nil)
	}
	if r.p.Validator == nil {
		return Check{ID: id, Status: StatusSkipped, Reason: reasonNotInIncrement,
			Message:     "The validator is not wired into doctor in this build.",
			Remediation: "Run whr validate for the configuration result."}
	}
	rem := r.b.remaining()
	if rem == 0 {
		return exhausted(id)
	}
	vctx, cancel := context.WithTimeout(r.ctx, rem)
	defer cancel()
	out, err := r.p.Validator(vctx, r.cwd)
	if err != nil {
		return unknown(id, reasonNotMeasurable, "The validator did not finish.", "Run whr validate for the configuration result.", nil)
	}
	ev := map[string]any{"error_findings": out.Errors, "warning_findings": out.Warnings}
	if out.Errors > 0 {
		return failed(id, "workharbor.json has "+strconv.Itoa(out.Errors)+" error finding(s).", "Run whr validate for the findings and fix every error.", ev)
	}
	return passed(id, "workharbor.json validates with no error finding.", ev)
}
