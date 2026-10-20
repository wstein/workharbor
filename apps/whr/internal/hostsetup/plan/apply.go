//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

const StepBudget = 900 * time.Second

type BackupAudit struct{ Path, SHA256 string }
type DiffAudit struct{ Added, Removed int }
type StepOutcome struct {
	Audit   profile.StepAudit
	Changed bool
	Reason  string
	Backups []BackupAudit
	Diff    *DiffAudit
}

// StepRunner is the injected concrete action boundary. Implementations must
// honor context and the per-step budget, use the supplied run ID for backups,
// and complete owned child cleanup before
// returning (process adapters retain T003's 30-second interrupt grace and 64KiB
// output bound), and return only audit metadata, never output/env contents.
// T003/T004 adapters are intentionally separate from this orchestration core:
// pkgmgr already imports plan, so importing it here would introduce a cycle.
type StepRunner interface {
	ApplyStep(context.Context, Step, string, time.Duration) (StepOutcome, error)
}
type ApplyRequest struct {
	Input                             Input
	ExpectedHash, RunID, Mode, Answer string
	Confirmed, DryRun                 bool
	UID                               int
	SUDOUserSet                       bool
	ConfirmedItems                    map[string]bool
}

// ApplyResult is internal source evidence, not a command envelope or an AP-6
// reason placement. Profile writes count as mutations in partial-failure audit.
type ApplyResult struct {
	Steps              []StepOutcome
	Applied, Remaining []string
	ExitCode           int
	ProfileChanged     bool
	ProtocolWarning    bool
}
type ApplyEngine struct {
	Store  *profile.Store
	Clock  detect.Clock
	Runner StepRunner
}

func refusal(reason string) error { return errors.New("host_setup." + reason) }
func interrupted(ctx context.Context) int {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return 71
	}
	return 72
}
func chosen(in Input) (map[string]string, []string, []string) {
	values := map[string]string{}
	selected, off := []string{}, []string{}
	for _, item := range in.Catalogue.Items {
		on := item.SelectedByDefault
		if v, ok := in.Selected[item.ID]; ok {
			on = v
		}
		if in.OptedOut[item.ID] {
			on = false
		}
		if on {
			values[item.ID] = "selected"
			selected = append(selected, item.ID)
		} else {
			values[item.ID] = "opted_out"
			off = append(off, item.ID)
		}
	}
	if _, exists := in.Selected["git-config"]; exists || in.OptedOut["git-config"] {
		if in.Selected["git-config"] && !in.OptedOut["git-config"] {
			values["git-config"] = "selected"
			selected = append(selected, "git-config")
		} else {
			values["git-config"] = "opted_out"
			off = append(off, "git-config")
		}
	}
	return values, selected, off
}
func journalOutcome(run *profile.Run, s Step, out StepOutcome, now time.Time) error {
	for _, backup := range out.Backups {
		if err := run.Backup(backup.Path, backup.SHA256, now); err != nil {
			return err
		}
	}
	if out.Diff != nil {
		if err := run.Diff(s.ID, s.Item, out.Diff.Added, out.Diff.Removed, now); err != nil {
			return err
		}
	}
	return run.Step(out.Audit, now)
}
func (e ApplyEngine) Apply(ctx context.Context, r ApplyRequest) (out ApplyResult, err error) {
	out.Applied = []string{}
	out.Remaining = []string{}
	out.Steps = []StepOutcome{}
	fail := func(code int, reason string) (ApplyResult, error) { out.ExitCode = code; return out, refusal(reason) }
	if ctx.Err() != nil {
		out.ExitCode = interrupted(ctx)
		return out, ctx.Err()
	}
	if r.Input.Platform != "darwin-arm64" && r.Input.Platform != "darwin-amd64" {
		return fail(68, "platform_unsupported")
	}
	if r.UID == 0 || r.SUDOUserSet {
		return fail(68, "running_as_root")
	}
	if r.Mode != "setup" && r.Mode != "fix" && r.Mode != "doctor" {
		return fail(64, "flag_not_valid_here")
	}
	sealed, buildErr := Build(r.Input)
	if buildErr != nil {
		return fail(74, "catalogue_invalid")
	}
	if Recheck(sealed, r.ExpectedHash) != nil {
		return fail(68, "plan_changed")
	}
	known := map[string]bool{"git-config": true}
	for _, item := range r.Input.Catalogue.Items {
		known[item.ID] = true
	}
	for id := range r.ConfirmedItems {
		if !known[id] {
			return fail(64, "unknown_item")
		}
	}
	if r.DryRun || r.Mode == "doctor" || r.Answer == "no" {
		return out, nil
	}
	if !r.Confirmed {
		return fail(70, "confirmation_missing")
	}
	if e.Store == nil || e.Clock == nil || len(sealed.Steps) > 0 && e.Runner == nil {
		return fail(74, "catalogue_invalid")
	}
	values, selected, off := chosen(r.Input)
	catalogueVersion := r.Input.Catalogue.CatalogueVersion
	command := "setup"
	if r.Mode == "fix" {
		command = "doctor_fix"
	}
	run, beginErr := e.Store.Begin(profile.Start{RunID: r.RunID, Command: command, Platform: r.Input.Platform, CatalogueVersion: catalogueVersion, Answer: r.Answer, Selected: selected, OptedOut: off}, e.Clock.Now())
	if beginErr != nil {
		if errors.Is(beginErr, profile.ErrLocked) {
			return fail(68, "run_locked")
		}
		return fail(68, "protocol_unwritable")
	}
	out.ProtocolWarning = run.Warning
	changed := false
	// Cleanup does not inherit a cancelled step context. A durable journal failure
	// stops all future actions; Close always releases this run's flock.
	defer func() {
		if out.ExitCode != 0 && out.ExitCode != 74 && changed {
			out.ExitCode = 73
		}
		if finishErr := run.Finish(out.ExitCode, out.Applied, out.Remaining, e.Clock.Now()); finishErr != nil {
			err = refusal("protocol_unwritable")
			if changed {
				out.ExitCode = 73
			} else {
				out.ExitCode = 68
			}
		}
		if closeErr := run.Close(); closeErr != nil {
			err = refusal("protocol_unwritable")
			if changed {
				out.ExitCode = 73
			} else {
				out.ExitCode = 68
			}
		}
	}()
	old, readErr := e.Store.ReadProfile()
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return fail(68, "target_unsafe")
	}
	completed := map[string]bool{}
	// Consent maps are frozen before any injected action can mutate caller inputs.
	confirmations := map[string]bool{}
	for id, v := range r.ConfirmedItems {
		confirmations[id] = v
	}
	for _, step := range sealed.Steps {
		result := StepOutcome{Audit: profile.StepAudit{ID: step.ID, Item: step.Item, Kind: step.Kind, Result: "blocked"}}
		switch {
		case ctx.Err() != nil:
			result.Audit.Result = "skipped"
			result.Reason = "interrupted"
			out.ExitCode = interrupted(ctx)
		case step.Blocked != "":
			result.Reason = step.Blocked
		default:
			ready := true
			for _, id := range step.DependsOn {
				if !completed[id] {
					ready = false
					break
				}
			}
			if !ready {
				result.Reason = "dependency_failed"
			} else if step.Class == "modify" && !confirmations[step.Item] {
				result.Audit.Result = "declined"
				result.Reason = "confirmation_required"
			} else {
				// A fresh budget applies to each step, not to the entire multi-step run.
				// The parent context still bounds the complete run and every child step.
				started := e.Clock.Now()
				stepCtx, cancel := context.WithTimeout(ctx, StepBudget)
				dispatch := step
				dispatch.DependsOn = append([]string(nil), step.DependsOn...)
				got, actionErr := e.Runner.ApplyStep(stepCtx, dispatch, r.RunID, StepBudget)
				stepErr := stepCtx.Err()
				cancel()
				result = got
				result.Reason = ""
				result.Audit.ID = step.ID
				result.Audit.Item = step.Item
				result.Audit.Kind = step.Kind
				// Errors are classified without retaining raw adapter error text.
				if actionErr != nil && result.Audit.Result == "" {
					result.Audit.Result = "blocked"
					result.Reason = "step_failed"
				}
				if ctx.Err() != nil {
					out.ExitCode = interrupted(ctx)
					if result.Audit.Result != "applied" && result.Audit.Result != "unchanged" {
						result.Reason = "interrupted"
						if len(result.Audit.Argv) == 0 && result.Audit.ExitStatus == nil {
							result.Audit.Result = "skipped"
						}
					}
				} else if errors.Is(stepErr, context.DeadlineExceeded) || e.Clock.Now().Sub(started) >= StepBudget {
					result.Reason = "step_timeout"
					if result.Audit.Result == "applied" {
						result.Changed = true
					}
					if len(result.Audit.Argv) == 0 && result.Audit.ExitStatus == nil {
						result.Audit.Result = "skipped"
					} else {
						result.Audit.Result = "failed"
					}
					if out.ExitCode < 71 {
						out.ExitCode = 71
					}
				} else if actionErr != nil {
					result.Reason = "step_failed"
					if result.Audit.Result == "applied" {
						result.Changed = true
					}
					if len(result.Audit.Argv) == 0 && result.Audit.ExitStatus == nil {
						result.Audit.Result = "blocked"
					} else {
						result.Audit.Result = "failed"
					}
				}
			}
		}
		if result.Audit.Result == "applied" {
			result.Changed = true
		}
		changed = changed || result.Changed
		if recordErr := journalOutcome(run, step, result, e.Clock.Now()); recordErr != nil {
			// Never return rejected metadata (including an arbitrary argv/error canary).
			result = StepOutcome{Audit: profile.StepAudit{ID: step.ID, Item: step.Item, Kind: step.Kind, Result: "blocked"}, Reason: "protocol_unwritable", Changed: result.Changed}
			out.Steps = append(out.Steps, result)
			out.Remaining = append(out.Remaining, step.ID)
			for _, pending := range sealed.Steps[len(out.Steps):] {
				out.Remaining = append(out.Remaining, pending.ID)
			}
			return fail(68, "protocol_unwritable")
		}
		out.Steps = append(out.Steps, result)
		successful := (result.Audit.Result == "applied" || result.Audit.Result == "unchanged") && result.Reason != "step_timeout" && result.Reason != "interrupted" && result.Reason != "step_failed"
		completed[step.ID] = successful
		if result.Audit.Result == "applied" {
			out.Applied = append(out.Applied, step.ID)
		}
		if !successful && result.Audit.Result != "declined" && result.Audit.Result != "opted_out" {
			out.Remaining = append(out.Remaining, step.ID)
			if out.ExitCode == 0 {
				out.ExitCode = 68
			}
		}
	}
	// An interruption stops before the next managed operation, including profile
	// persistence. Otherwise remember the confirmed choices even on partial work.
	if ctx.Err() != nil {
		out.ExitCode = interrupted(ctx)
	} else {
		next := profile.HostProfile{SchemaVersion: 1, CatalogueVersion: catalogueVersion, Updated: e.Clock.Now().UTC().Format(time.RFC3339Nano), Items: values}
		profileChanged, saveErr := run.StoreProfile(next)
		if saveErr != nil {
			return fail(68, "target_unsafe")
		}
		out.ProfileChanged = profileChanged
		changed = changed || profileChanged
		if profileChanged {
			// Existing item IDs audit each changed selection; no extra public item or
			// envelope field is introduced for profile bookkeeping.
			for _, id := range append(append([]string(nil), selected...), off...) {
				if old.CatalogueVersion == next.CatalogueVersion && old.Items[id] == values[id] {
					continue
				}
				audit := profile.StepAudit{ID: id + ".profile_write", Kind: "profile_write", Item: id, Result: "applied"}
				if recordErr := run.Step(audit, e.Clock.Now()); recordErr != nil {
					return fail(68, "protocol_unwritable")
				}
				out.Applied = append(out.Applied, audit.ID)
			}
		}
	}
	return out, nil
}
