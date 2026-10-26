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
	local, dryRun, yes, interactive bool
	input                           io.Reader
	diagnostics                     io.Writer
}

func (a *app) localPromoteOptions(c *cobra.Command) localPromoteInvocation {
	v := localPromoteInvocation{input: a.env.Stdin, diagnostics: a.env.Stderr}
	v.local, _ = c.Flags().GetBool("local")
	v.dryRun, _ = c.Flags().GetBool("dry-run")
	v.yes, _ = c.Flags().GetBool("yes")
	v.interactive = !a.jsonMode() && a.env.InputTerminal && a.env.IsTerminal(a.env.Stderr) && a.env.Stdin != nil
	return v
}

// LocalPromoteHandler confirms the receipt-bound replacement and complete
// throwaway candidate disposal with one normal mutation confirmation.
func LocalPromoteHandler(prepare func(context.Context, localdocker.PromoteOptions) (localdocker.LifecycleTransaction, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localPromoteKey{}).(localPromoteInvocation)
		if !v.local || len(call.Args) != 1 || strings.TrimSpace(call.Args[0]) == "" {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "use workspace promote --local <exact-name|full-id>")
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
			return Result{Value: preview, Human: promoteHuman(preview) + "Apply requires confirmation or --yes.\n"}, nil
		}
		if v.interactive && !v.yes {
			if _, err = io.WriteString(v.diagnostics, promoteHuman(preview)+"Pause input writers for every execution phase. Promote and dispose of all previewed candidate artifacts? [y/N] "); err != nil {
				return Result{}, err
			}
			answer, e := lifecycleAnswer(ctx, bufio.NewReaderSize(v.input, 1025))
			if e != nil {
				return Result{}, e
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionRefused, "promote confirmation declined")
			}
		}
		result, err := tx.Apply(ctx, localdocker.LifecycleConsent{Confirmed: true, Quiescence: true})
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
	return lifecycleHuman(r) + candidateDisposalHelp + "Required result refs are verified in the original repository before clone disposal.\nPrimary replacement ends all whr shell panes and other exec sessions. The original primary home is never a cleanup target. Trusted container hooks may write the original workspace.\n"
}
