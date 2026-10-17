// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// humanFailure is a failure whose human-mode report goes to standard output
// before the "whr:" line (validate lists its findings that way); in --json mode
// the report travels as the envelope result instead.
type humanFailure struct {
	err   *exitcode.Error
	human string
}

func (h *humanFailure) Error() string { return h.err.Error() }
func (h *humanFailure) Unwrap() error { return h.err }

// validateHandler implements `whr workspace validate [dir]` (FR-054/FR-055).
func validateHandler(ctx context.Context, call Call) (Result, error) {
	if len(call.Args) > 1 {
		return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "validate takes at most one directory").
			WithRemediation("run whr validate [dir]")
	}
	dir := "."
	if len(call.Args) == 1 {
		dir = call.Args[0]
	}
	report, err := workspacecfg.Validate(ctx, dir, workspacecfg.Options{})
	switch {
	case errors.Is(err, workspacecfg.ErrNotFound):
		return Result{}, exitcode.Errorf(exitcode.PathTargetAbsent, "the directory %s does not exist", dir).
			WithRemediation("pass an existing directory or run whr validate in one")
	case errors.Is(err, workspacecfg.ErrNotDirectory):
		return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "%s is not a directory", dir).
			WithRemediation("pass the directory that holds workharbor.json")
	case err != nil:
		return Result{}, err // context end maps to interrupted or timeout
	}
	human := renderReport(report)
	if report.Valid {
		return Result{Value: report, Human: human}, nil
	}
	e := exitcode.Errorf(exitcode.PathConflict, "the configuration is invalid: %s", countSummary(report.Counts)).
		WithRemediation("fix the errors above and run whr validate again")
	e.Result = report
	return Result{}, &humanFailure{err: e, human: human}
}

func countSummary(c workspacecfg.Counts) string {
	return fmt.Sprintf("%s, %s, %s", plural(c.Error, "error"), plural(c.Warning, "warning"), plural(c.Info, "note"))
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// renderReport writes the human report. Every name, path and message may come
// from the hostile directory, so all of it passes terminal.Escape.
func renderReport(r *workspacecfg.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Directory: %s\n", terminal.Escape(r.Directory))
	for _, f := range r.Findings {
		subject := ""
		if f.Subject != "" {
			subject = " " + terminal.Escape(f.Subject) + ":"
		}
		fmt.Fprintf(&b, "%-7s %s%s %s\n", f.Severity, f.Code, subject, terminal.Escape(f.Message))
		if f.Remediation != "" {
			fmt.Fprintf(&b, "        hint: %s\n", terminal.Escape(f.Remediation))
		}
	}
	if r.Truncated {
		fmt.Fprintf(&b, "(only the first %d findings are listed; the counts below are exact)\n", workspacecfg.MaxFindings)
	}
	verdict := "valid"
	if !r.Valid {
		verdict = "invalid"
	}
	fmt.Fprintf(&b, "Result: %s (%s)\n", verdict, countSummary(r.Counts))
	fmt.Fprintf(&b, "Provider admission: %s (validation does not claim the configuration can be provisioned)\n",
		strings.ReplaceAll(r.ProviderAdmission, "_", " "))
	return b.String()
}
