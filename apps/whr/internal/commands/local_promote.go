// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

type localPromoteKey struct{}
type localPromoteInvocation struct {
	local, dryRun, yes, volumes, force, interactive bool
	input                                           io.Reader
	diagnostics                                     io.Writer
}

func (a *app) localPromoteOptions(c *cobra.Command) localPromoteInvocation {
	v := localPromoteInvocation{input: a.env.Stdin, diagnostics: a.env.Stderr}
	v.local, _ = c.Flags().GetBool("local")
	v.dryRun, _ = c.Flags().GetBool("dry-run")
	v.yes, _ = c.Flags().GetBool("yes")
	v.volumes, _ = c.Flags().GetBool("volumes")
	v.force, _ = c.Flags().GetBool("force")
	v.interactive = !a.jsonMode() && a.env.InputTerminal && a.env.IsTerminal(a.env.Stderr) && a.env.Stdin != nil
	return v
}

// LocalPromoteHandler exposes the receipt-bound promotion library unchanged.
// Explicit deletion consent is checked before preparation, never inferred.
func LocalPromoteHandler(prepare func(context.Context, localdocker.PromoteOptions) (localdocker.LifecycleTransaction, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localPromoteKey{}).(localPromoteInvocation)
		if !v.local || len(call.Args) != 1 || strings.TrimSpace(call.Args[0]) == "" {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "use workspace promote --local <exact-name|full-id>")
		}
		if v.volumes && !v.force || !v.dryRun && (!v.volumes || !v.force) {
			return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "promote apply requires explicit --volumes and --force; --yes supplies neither")
		}
		if !v.dryRun && !v.yes && !v.interactive {
			return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended promote requires --yes (or --dry-run)")
		}
		tx, err := prepare(ctx, localdocker.PromoteOptions{Target: call.Args[0], Diagnostics: v.diagnostics})
		if err != nil {
			return Result{}, err
		}
		defer func() {
			if e := tx.Close(); e != nil && v.diagnostics != nil {
				fmt.Fprintln(v.diagnostics, "whr: warning: preparation cleanup: "+terminal.Escape(e.Error()))
			}
		}()
		preview := tx.Preview()
		if v.dryRun {
			return Result{Value: preview, Human: promoteHuman(preview)}, nil
		}
		if v.interactive && !v.yes {
			if _, err = io.WriteString(v.diagnostics, promoteHuman(preview)+"Pause input writers for every execution phase. Promote and remove the exact candidate and candidate home? [y/N] "); err != nil {
				return Result{}, err
			}
			answer, e := lifecycleAnswer(ctx, bufio.NewReaderSize(v.input, 1025))
			if e != nil {
				return Result{}, e
			}
			if answer != "y" && answer != "yes" {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionRefused, "promote confirmation declined")
			}
		}
		result, err := tx.Apply(ctx, localdocker.LifecycleConsent{Confirmed: true, Volumes: v.volumes, Force: v.force, Quiescence: true})
		res := Result{Value: result, Human: promoteHuman(result)}
		if err != nil {
			failure := asError(err)
			copy := *failure
			copy.Result = result
			return res, &copy
		}
		return res, nil
	}
}
func promoteHuman(r localdocker.LifecycleResult) string {
	return lifecycleHuman(r) + "Primary replacement ends all whr shell panes and other exec sessions. The original primary home is preserved; only the candidate home is a cleanup target. Trusted container hooks may write the original workspace.\n"
}
