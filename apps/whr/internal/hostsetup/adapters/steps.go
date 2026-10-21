//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
// Package adapters binds reviewed concrete setup engines to plan orchestration.
package adapters

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/dotfiles"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

type Runner struct {
	brew     pkgmgr.Engine
	request  pkgmgr.Request
	files    *dotfiles.Engine
	previews map[string]*dotfiles.Preview
}

// New binds fresh same-engine previews to the already-shown plan. It neither
// writes nor executes; stale preview metadata cannot inherit prior confirmation.
func New(brew pkgmgr.Engine, request pkgmgr.Request, files *dotfiles.Engine) (*Runner, error) {
	sealed, err := plan.Build(request.Input)
	if err != nil || plan.Recheck(sealed, request.ExpectedHash) != nil {
		return nil, errors.New("host setup plan changed")
	}
	r := &Runner{brew: brew, request: request, files: files, previews: map[string]*dotfiles.Preview{}}
	// Consent/environment maps belong to this adapter after construction.
	r.request.Policy.Confirmed = map[string]bool{}
	for id, v := range request.Policy.Confirmed {
		r.request.Policy.Confirmed[id] = v
	}
	r.request.Environment = map[string]string{}
	for k, v := range request.Environment {
		r.request.Environment[k] = v
	}
	for _, step := range sealed.Steps {
		if step.Blocked != "" || step.Kind != "dotfile_create" && step.Kind != "dotfile_replace" {
			continue
		}
		if files == nil {
			return nil, errors.New("dotfile engine unavailable")
		}
		for _, item := range request.Input.Catalogue.Items {
			if item.ID != step.Item {
				continue
			}
			if item.Config == nil {
				return nil, errors.New("dotfile preview unavailable")
			}
			preview, previewErr := files.Preview(*item.Config, request.Input.Catalogue.Templates)
			if previewErr != nil || preview.Target() != step.Target || preview.BeforeSHA256() != step.BeforeSHA256 || preview.Class() != step.Class {
				return nil, errors.New("host setup plan changed")
			}
			r.previews[step.ID] = preview
		}
	}
	return r, nil
}
func (r *Runner) ApplyStep(ctx context.Context, step plan.Step, runID string, budget time.Duration, guard plan.StepGuard) (plan.StepOutcome, error) {
	if step.Kind == "brew_install" || step.Kind == "brew_upgrade" {
		return (pkgmgr.StepAdapter{Engine: r.brew, Request: r.request}).ApplyStep(ctx, step, runID, budget, guard)
	}
	out := plan.StepOutcome{Audit: profile.StepAudit{ID: step.ID, Item: step.Item, Kind: step.Kind, Result: "blocked"}}
	refuse := func() (plan.StepOutcome, error) { return out, errors.New("dotfile step refused") }
	if (r.request.Input.Platform != "darwin-arm64" && r.request.Input.Platform != "darwin-amd64") || guard == nil || budget != plan.StepBudget || r.files == nil || r.request.Policy.UID == 0 || r.request.Policy.SUDOUserSet || r.request.Policy.DryRun || !r.request.Policy.Yes || (r.request.Policy.Mode != "setup" && r.request.Policy.Mode != "fix") {
		return refuse()
	}
	if ctx.Err() != nil {
		out.Audit.Result = "skipped"
		return out, ctx.Err()
	}
	sealed, err := plan.Build(r.request.Input)
	if err != nil || plan.Recheck(sealed, r.request.ExpectedHash) != nil {
		return refuse()
	}
	matched := false
	for _, shown := range sealed.Steps {
		if reflect.DeepEqual(shown, step) {
			matched = true
			break
		}
	}
	if !matched || step.Blocked != "" {
		return refuse()
	}
	for _, id := range step.DependsOn {
		if !guard.Completed(id) {
			return out, nil
		}
	}
	preview := r.previews[step.ID]
	if preview == nil {
		return refuse()
	}
	if step.Class == "modify" && !r.request.Policy.Confirmed[step.Item] {
		out.Audit.Result = "declined"
		return out, nil
	}
	if err = guard.Checkpoint(); err != nil {
		return out, err
	}
	if ctx.Err() != nil {
		out.Audit.Result = "skipped"
		return out, ctx.Err()
	}
	decision := dotfiles.Create
	if step.Kind == "dotfile_replace" {
		decision = dotfiles.Replace
	} else if step.Kind != "dotfile_create" {
		return refuse()
	}
	applied, actionErr := r.files.Apply(preview, dotfiles.Options{Decision: decision, Target: step.Target, RunID: runID})
	out.Audit.Result = applied.Status
	if out.Audit.Result == "" {
		out.Audit.Result = "blocked"
	}
	// An owned backup write counts even if verification or later recovery fails.
	out.Changed = applied.Status == "applied" || applied.BackupCreated
	if applied.Backup != nil {
		out.Backups = []plan.BackupAudit{{Path: applied.Backup.Path, SHA256: applied.Backup.SHA256}}
	}
	if applied.Status == "applied" {
		added, removed := preview.DiffCounts()
		out.Diff = &plan.DiffAudit{Added: added, Removed: removed}
	}
	return out, actionErr
}
