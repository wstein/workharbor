// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

type initContextKey struct{}
type initInvocation struct {
	options     scaffold.Options
	err         error
	diagnostics io.Writer
}

// initHandler consumes the same typed proposal used by the later wizard.
// Preview goes to diagnostics before any write; stdout gets only the outcome.
func initHandler(ctx context.Context, call Call) (Result, error) {
	return initWithApply(ctx, call, func(ctx context.Context, p *scaffold.Proposal) (scaffold.Preview, error) { return p.Apply(ctx) })
}

// initWithApply is a private failure-output seam; it exposes no CLI flag.
func initWithApply(ctx context.Context, call Call, apply func(context.Context, *scaffold.Proposal) (scaffold.Preview, error)) (Result, error) {
	invocation, ok := ctx.Value(initContextKey{}).(initInvocation)
	if !ok {
		return Result{}, exitcode.Errorf(exitcode.PathUnclassified, "init options are unavailable")
	}
	if invocation.err != nil {
		return Result{}, invocation.err
	}
	if len(call.Args) > 1 {
		return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "init takes at most one directory")
	}
	directory := "."
	if len(call.Args) == 1 {
		directory = call.Args[0]
	}
	snapshot, err := scaffold.Discover(ctx, directory)
	if err != nil {
		return Result{}, initFailure(ctx, directory, invocation.options.DryRun, nil, err)
	}
	proposal, err := scaffold.Build(snapshot, invocation.options)
	if err != nil {
		return Result{}, initFailure(ctx, directory, invocation.options.DryRun, nil, err)
	}
	if _, err = io.WriteString(invocation.diagnostics, proposal.HumanPreview()); err != nil {
		return Result{}, exitcode.Errorf(exitcode.PathUnclassified, "writing init preview failed: %v", err)
	}
	result, err := apply(ctx, proposal)
	if err != nil {
		if exitcode.FromError(err) == exitcode.PartialFailure {
			for _, name := range result.Remaining {
				fmt.Fprintf(invocation.diagnostics, "remaining: %s\n", terminal.Escape(name))
			}
			fmt.Fprintln(invocation.diagnostics, "Inspect and recover every remaining path before retrying init.")
		}
		return Result{}, initFailure(ctx, directory, invocation.options.DryRun, &result, err)
	}
	status := "Workspace configuration unchanged"
	if result.DryRun {
		status = "Dry run: no files written"
	} else {
		for _, a := range result.Actions {
			if a.Applied {
				status = "Initialized workspace configuration"
				break
			}
		}
	}
	return Result{Value: result, Human: fmt.Sprintf("%s in %s.\n", status, terminal.Escape(directory))}, nil
}

// initFailure preserves the writer's structured result when present. A failure
// before a proposal exists uses the read-only Git-free content report; it never
// runs Git or treats invalid preserved configuration as a generated proposal.
func initFailure(ctx context.Context, directory string, dryRun bool, preview *scaffold.Preview, cause error) error {
	err := asError(cause)
	if err.Result != nil {
		return err
	}
	if preview != nil {
		err.Result = *preview
		return err
	}
	if err.Code() != exitcode.Conflict && err.Code() != exitcode.PartialFailure {
		return err
	}
	report, readErr := workspacecfg.Validate(ctx, directory, workspacecfg.Options{GitFree: true})
	if readErr != nil {
		report = workspacecfg.NewReport(directory, []workspacecfg.Finding{{Code: "manifest.unsafe_path", Severity: workspacecfg.SevError, Message: "configuration could not be safely inspected"}})
	}
	err.Result = scaffold.Preview{Directory: directory, DryRun: dryRun, Actions: []scaffold.Action{}, Validation: report, RuntimeEnforcement: "not_in_this_release", GitChecks: "not_run", BundleSelectionUnavailable: true, NextSteps: []string{"Resolve the reported init conflict before retrying; no proposal was written."}}
	return err
}
