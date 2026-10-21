//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package pkgmgr

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

// StepAdapter reuses the existing T003 action engine under the orchestration
// owner's journal and verified dependency evidence; it never starts another run.
type StepAdapter struct {
	Engine  Engine
	Request Request
}
type startingJournal struct{ guard plan.StepGuard }

func (j startingJournal) Record(ctx context.Context, e Event) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e.Stage != "starting" {
		return errors.New("unexpected package-manager journal stage")
	}
	return j.guard.Checkpoint()
}
func (startingJournal) Close(context.Context) error { return nil }
func (a StepAdapter) ApplyStep(ctx context.Context, step plan.Step, _ string, budget time.Duration, guard plan.StepGuard) (plan.StepOutcome, error) {
	out := plan.StepOutcome{Audit: profile.StepAudit{ID: step.ID, Item: step.Item, Kind: step.Kind, Result: "blocked"}}
	refuse := func() (plan.StepOutcome, error) { return out, errors.New("package-manager step refused") }
	p := a.Request.Policy
	if guard == nil || budget != plan.StepBudget || p.UID == 0 || p.SUDOUserSet || p.DryRun || !p.Yes || (p.Mode != "setup" && p.Mode != "fix") || (a.Request.Input.Platform != "darwin-arm64" && a.Request.Input.Platform != "darwin-amd64") {
		return refuse()
	}
	if ctx.Err() != nil {
		out.Audit.Result = "skipped"
		return out, ctx.Err()
	}
	sealed, err := plan.Build(a.Request.Input)
	if err != nil || plan.Recheck(sealed, a.Request.ExpectedHash) != nil {
		return refuse()
	}
	matched := false
	for _, shown := range sealed.Steps {
		if reflect.DeepEqual(shown, step) {
			matched = true
			break
		}
	}
	if !matched || step.Blocked != "" || (step.Kind != "brew_install" && step.Kind != "brew_upgrade") {
		return refuse()
	}
	if step.Class == "modify" && !p.Confirmed[step.Item] {
		out.Audit.Result = "declined"
		return out, nil
	}
	if a.Engine.Exec == nil || a.Engine.FS == nil || a.Engine.Clock == nil || a.Engine.Detector == nil {
		return refuse()
	}
	byID := map[string]catalogue.Item{}
	for _, item := range a.Request.Input.Catalogue.Items {
		byID[item.ID] = item
	}
	item := byID[step.Item]
	if item.Tool.Source.MayElevate {
		return refuse()
	}
	completed := map[string]bool{}
	for _, id := range step.DependsOn {
		completed[id] = guard.Completed(id)
	}
	if !dependenciesReady(ctx, step, item, byID, completed, a.Engine.Detector) {
		return out, nil
	}
	sr, argv, stepErr := a.Engine.applyStep(ctx, step, item, a.Request.Environment, startingJournal{guard: guard})
	out.Audit.Result = sr.Status
	out.Audit.Argv = argv
	out.Audit.ExitStatus = sr.Exit
	// Homebrew invocation is the network-capable package operation; no inherited
	// environment, child output or password is included in its audit payload.
	out.Audit.Network = len(argv) > 0
	out.Changed = sr.Changed
	out.Reason = sr.Reason
	// A cleanup failure keeps ownership of the started process; hand it to the
	// caller unchanged so it can reconcile the child.
	if stepErr != nil {
		return out, stepErr
	}
	// Preserve typed timeouts/cancellation without exporting private error text.
	if sr.Reason == "deadline_exceeded" {
		return out, context.DeadlineExceeded
	}
	if sr.Reason == "cancelled" && ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, nil
}
