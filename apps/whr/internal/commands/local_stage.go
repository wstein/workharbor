// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

type localStageKey struct{}
type localStageInvocation struct {
	local, dryRun, yes, interactive bool
	trust                           string
	options                         localdocker.StageOptions
	input                           io.Reader
	diagnostics                     io.Writer
}

func (a *app) localStageOptions(c *cobra.Command) localStageInvocation {
	v := localStageInvocation{input: a.env.Stdin, diagnostics: a.env.Stderr}
	v.local, _ = c.Flags().GetBool("local")
	v.dryRun, _ = c.Flags().GetBool("dry-run")
	v.yes, _ = c.Flags().GetBool("yes")
	v.trust, _ = c.Flags().GetString("trust")
	v.options.Source, _ = c.Flags().GetString("source")
	v.options.Smoke, _ = c.Flags().GetBool("smoke")
	v.options.Diagnostics = a.env.Stderr
	v.interactive = !a.jsonMode() && a.env.InputTerminal && a.env.IsTerminal(a.env.Stderr) && a.env.Stdin != nil
	return v
}

// LocalStageHandler is the CLI adapter for the accepted local stage transaction.
// The library owns capture, native execution, home allocation and recovery.
func LocalStageHandler(prepare func(context.Context, localdocker.StageOptions) (localdocker.LifecycleTransaction, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localStageKey{}).(localStageInvocation)
		if !v.local || strings.TrimSpace(v.options.Source) == "" || len(call.Args) != 1 || strings.TrimSpace(call.Args[0]) == "" {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "use workspace stage --local --source <dir> <exact-name|full-id>")
		}
		if !v.dryRun && !v.interactive {
			if !v.yes {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended stage requires --yes (or --dry-run)")
			}
			if v.trust == "" {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended stage requires exact --trust <fingerprint>, separately from --yes")
			}
		}
		v.options.Target = call.Args[0]
		tx, err := prepare(ctx, v.options)
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
			return Result{Value: preview, Human: lifecycleHuman(preview)}, nil
		}
		if preview.Fingerprint == nil || *preview.Fingerprint == "" {
			return Result{}, exitcode.Errorf(exitcode.PathConflict, "stage preview has no source fingerprint")
		}
		if v.interactive {
			if _, err = io.WriteString(v.diagnostics, lifecycleHuman(preview)); err != nil {
				return Result{}, err
			}
		}
		reader := bufio.NewReaderSize(v.input, 1025)
		if v.trust == "" && v.interactive {
			if _, err = io.WriteString(v.diagnostics, "Trust source fingerprint "+terminal.Escape(*preview.Fingerprint)+"? Enter the exact fingerprint: "); err != nil {
				return Result{}, err
			}
			answer, e := lifecycleAnswer(ctx, reader)
			if e != nil {
				return Result{}, e
			}
			if answer != *preview.Fingerprint {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionRefused, "source fingerprint trust declined")
			}
			v.trust = answer
		}
		if v.trust != *preview.Fingerprint {
			return Result{}, exitcode.Errorf(exitcode.PathConflict, "source fingerprint does not match --trust")
		}
		if !v.yes {
			if _, err = io.WriteString(v.diagnostics, "Stage this candidate with the disclosed effects and pause-writers condition? [y/N] "); err != nil {
				return Result{}, err
			}
			answer, e := lifecycleAnswer(ctx, reader)
			if e != nil {
				return Result{}, e
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionRefused, "stage confirmation declined")
			}
		}
		result, err := tx.Apply(ctx, localdocker.LifecycleConsent{TrustDigest: v.trust, Confirmed: true, Quiescence: true})
		res := Result{Value: result, Human: lifecycleHuman(result)}
		if err != nil {
			failure := asError(err)
			copy := *failure
			copy.Result = result
			return res, &copy
		}
		return res, nil
	}
}

func lifecycleAnswer(ctx context.Context, reader *bufio.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	line, err := reader.ReadSlice('\n')
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	if err != nil || len(line) > 1024 {
		return "", exitcode.Errorf(exitcode.PathHumanDecisionRefused, "confirmation declined: a complete bounded answer is required")
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"), nil
}

func lifecycleHuman(r localdocker.LifecycleResult) string {
	var b strings.Builder
	if r.Status == "preview" {
		b.WriteString("Pause input writers for capture and every execution phase; trusted hooks may write their previewed workspace.\nHashes cannot detect a writer that changes and restores bytes during a phase.\n")
	}
	fmt.Fprintf(&b, "status: %s\nphase: %s\n", terminal.Escape(r.Status), terminal.Escape(r.Phase))
	if r.Fingerprint != nil {
		fmt.Fprintf(&b, "fingerprint: %s\n", terminal.Escape(*r.Fingerprint))
	}
	for _, entry := range []struct {
		name  string
		value *localdocker.StageResource
	}{{"primary", r.Primary}, {"candidate", r.Candidate}, {"candidate home", r.Home}} {
		if entry.value == nil {
			continue
		}
		v := entry.value
		fmt.Fprintf(&b, "%s: daemon=%s", entry.name, terminal.Escape(v.DaemonID))
		if v.Name != nil {
			fmt.Fprintf(&b, " name=%s", terminal.Escape(*v.Name))
		}
		if v.ID != nil {
			fmt.Fprintf(&b, " id=%s", terminal.Escape(*v.ID))
		}
		b.WriteByte('\n')
	}
	if r.UnverifiedReason != nil {
		fmt.Fprintf(&b, "unverified reason: %s\n", terminal.Escape(*r.UnverifiedReason))
	}
	if r.RecoveryID != nil {
		fmt.Fprintf(&b, "recovery: %s\n", terminal.Escape(*r.RecoveryID))
	}
	if r.Verification != nil {
		fmt.Fprintf(&b, "verification: %s %s\n", terminal.Escape(r.Verification.Policy), terminal.Escape(r.Verification.Status))
	}
	for _, repo := range r.Repositories {
		fmt.Fprintf(&b, "repository: %s -> %s (container %s)\n", terminal.Escape(repo.SourcePath), terminal.Escape(repo.ClonePath), terminal.Escape(repo.ContainerWorkspacePath))
	}
	for _, effect := range r.Effects {
		target, _ := json.Marshal(effect.Target)
		fmt.Fprintf(&b, "effect: %s %s %s\n", terminal.Escape(effect.Action), terminal.Escape(effect.State), terminal.Escape(string(target)))
	}
	for _, ref := range r.ResultRefs {
		fmt.Fprintf(&b, "result ref: %s %s -> %s %s (%s)\n", terminal.Escape(ref.RepositoryPath), terminal.Escape(ref.ExportRef), terminal.Escape(ref.TargetRef), terminal.Escape(ref.OID), terminal.Escape(ref.State))
	}
	for _, step := range r.Recovery {
		fmt.Fprintf(&b, "recovery step: %s %s\n", terminal.Escape(step.Action), terminal.Escape(step.Instruction))
	}
	return b.String()
}
