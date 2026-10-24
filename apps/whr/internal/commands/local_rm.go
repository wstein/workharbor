// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

type localRmKey struct{}
type localRmInvocation struct {
	local, dryRun, yes, interactive bool
	options                         localdocker.RemovalOptions
	input                           io.Reader
	diagnostics                     io.Writer
	err                             error
}

func (a *app) localRmOptions(c *cobra.Command) localRmInvocation {
	v := localRmInvocation{input: a.env.Stdin, diagnostics: a.env.Stderr}
	v.local, _ = c.Flags().GetBool("local")
	v.dryRun, _ = c.Flags().GetBool("dry-run")
	v.yes, _ = c.Flags().GetBool("yes")
	v.options.Role, _ = c.Flags().GetString("role")
	v.options.Label, _ = c.Flags().GetString("label")
	v.options.Volumes, _ = c.Flags().GetBool("volumes")
	v.options.Force, _ = c.Flags().GetBool("force")
	v.interactive = !a.jsonMode() && a.env.InputTerminal && a.env.IsTerminal(a.env.Stderr) && a.env.Stdin != nil
	if c.Flags().Changed("role") && v.options.Role != "staging" && v.options.Role != "temp" {
		v.err = exitcode.Errorf(exitcode.PathBadArguments, "--role must be staging or temp")
	}
	if c.Flags().Changed("label") && (v.options.Label == "" || strings.HasPrefix(v.options.Label, "=")) {
		v.err = exitcode.Errorf(exitcode.PathBadArguments, "--label requires a key or key=value")
	}
	return v
}

// RemovalTransaction separates inspected intent from mutation and allows callers
// to confirm the exact transaction the backend prepared.
type RemovalTransaction interface {
	Preview() localdocker.RemovalResult
	Apply(context.Context) (localdocker.RemovalResult, error)
}

// LocalRmHandler validates arguments and obtains confirmation before applying a
// local removal. The backend owns Docker inspection and volume invariants.
func LocalRmHandler(prepare func(context.Context, localdocker.RemovalOptions) (RemovalTransaction, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localRmKey{}).(localRmInvocation)
		if !v.local {
			return Result{}, exitcode.Errorf(exitcode.PathUnavailableCommand, "workspace.rm is not available yet: it waits for %s (owner_api); use --local for local removal", WaitsFor("workspace.rm"))
		}
		if v.err != nil {
			return Result{}, v.err
		}
		selectors := 0
		if len(call.Args) > 0 {
			selectors++
			v.options.Target = call.Args[0]
		}
		if v.options.Role != "" {
			selectors++
		}
		if v.options.Label != "" {
			selectors++
		}
		if len(call.Args) > 1 || selectors != 1 || len(call.Args) == 1 && strings.TrimSpace(v.options.Target) == "" {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "rm --local requires exactly one container id/name, --role or --label")
		}
		if v.options.Volumes && !v.options.Force {
			return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "home volume deletion requires --volumes and --force")
		}
		if !v.dryRun && !v.yes && !v.interactive {
			return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended rm requires --yes (or --dry-run)")
		}
		tx, err := prepare(ctx, v.options)
		if err != nil {
			return Result{}, err
		}
		preview := tx.Preview()
		preview.DryRun = v.dryRun
		if v.dryRun {
			return Result{Value: preview, Human: removalHuman(preview)}, nil
		}
		if v.interactive && !v.yes {
			if _, err := io.WriteString(v.diagnostics, removalHuman(preview)+"Remove these resources? [y/N] "); err != nil {
				return Result{}, err
			}
			// Read one bounded line from the existing input stream; malformed, absent,
			// unterminated and oversized answers fail closed without another prompt.
			answer, err := bufio.NewReader(io.LimitReader(v.input, 1025)).ReadString('\n')
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return Result{}, err
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if err != nil || len(answer) > 1024 || answer != "yes" && answer != "y" {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionRefused, "removal was not confirmed")
			}
		}
		result, err := tx.Apply(ctx)
		if err != nil {
			ee := asError(err)
			if ee.Result == nil {
				ee.Result = result
			}
			return Result{Human: removalHuman(result)}, ee
		}
		return Result{Value: result, Human: removalHuman(result)}, nil
	}
}

func removalHuman(r localdocker.RemovalResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "preserve_home_volume: %t\n", r.PreserveHomeVolume)
	for _, c := range r.Containers {
		fmt.Fprintf(&b, "container: %s %s (role: %s)\n", terminal.Escape(c.ID), terminal.Escape(c.Name), terminal.Escape(c.Role))
	}
	for _, v := range r.Volumes {
		fmt.Fprintf(&b, "volume: %s\n", terminal.Escape(v.Name))
		for _, m := range v.Mounts {
			fmt.Fprintf(&b, "  mount: %s at %s (read_only: %t)\n", terminal.Escape(m.ContainerID), terminal.Escape(m.Destination), m.ReadOnly)
		}
	}
	for _, v := range r.PreservedHomeVolumes {
		fmt.Fprintf(&b, "preserved home volume: %s\n", terminal.Escape(v))
	}
	for _, a := range r.Actions {
		fmt.Fprintf(&b, "%s %s: applied=%t attempted=%t\n", terminal.Escape(a.Kind), terminal.Escape(a.Target), a.Applied, a.Attempted)
	}
	return b.String()
}
