// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

type localLsKey struct{}
type localLsInvocation struct {
	local       bool
	role, label string
	err         error
}

func localLsOptions(c *cobra.Command) localLsInvocation {
	local, _ := c.Flags().GetBool("local")
	role, _ := c.Flags().GetString("role")
	label, _ := c.Flags().GetString("label")
	v := localLsInvocation{local: local, role: role, label: label}
	if c.Flags().Changed("label") && label == "" {
		v.err = exitcode.Errorf(exitcode.PathBadArguments, "--label requires a key or key=value")
	}
	if c.Flags().Changed("role") && role == "" {
		v.err = exitcode.Errorf(exitcode.PathBadArguments, "--role must be primary, staging or temp")
	}
	return v
}

// LocalLsHandler exposes only read-only local discovery. Without --local the
// existing remote owner_api contract still refuses before any local I/O.
func LocalLsHandler(discover func(context.Context) ([]localdocker.Workspace, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localLsKey{}).(localLsInvocation)
		if !v.local {
			return Result{}, exitcode.Errorf(exitcode.PathUnavailableCommand, "workspace.ls is not available yet: it waits for %s (owner_api); use --local for local discovery", WaitsFor("workspace.ls"))
		}
		if v.err != nil {
			return Result{}, v.err
		}
		if len(call.Args) != 0 {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "ls --local takes no arguments")
		}
		if _, err := localdocker.Filter(nil, v.role, v.label); err != nil {
			return Result{}, err
		}
		all, err := discover(ctx)
		if err != nil {
			return Result{}, err
		}
		workspaces, err := localdocker.Filter(all, v.role, v.label)
		if err != nil {
			return Result{}, err
		}
		return Result{Value: struct {
			Workspaces []localdocker.Workspace `json:"workspaces"`
		}{workspaces}, Human: localdocker.Human(workspaces)}, nil
	}
}
