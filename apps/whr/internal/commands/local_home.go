// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// HomeCopyTransaction is the previewed home clone/backup bound to one daemon.
type HomeCopyTransaction interface {
	Preview() localdocker.HomeCopyResult
	Apply(ctx context.Context, trust string) (localdocker.HomeCopyResult, error)
}

type localHomeKey struct{}
type localHomeInvocation struct {
	local, dryRun, yes, interactive bool
	trust                           string
	options                         localdocker.HomeCopyOptions
	input                           io.Reader
	diagnostics                     io.Writer
}

func (a *app) localHomeOptions(c *cobra.Command) localHomeInvocation {
	v := localHomeInvocation{input: a.env.Stdin, diagnostics: a.env.Stderr}
	v.local, _ = c.Flags().GetBool("local")
	v.dryRun, _ = c.Flags().GetBool("dry-run")
	v.yes, _ = c.Flags().GetBool("yes")
	v.trust, _ = c.Flags().GetString("trust")
	v.options.To, _ = c.Flags().GetString("to")
	if c.Flags().Lookup("exclude") != nil {
		v.options.Excludes, _ = c.Flags().GetStringArray("exclude")
		v.options.IncludeCredentials, _ = c.Flags().GetBool("include-credentials")
	}
	if a.env.IsTerminal(a.env.Stderr) && !a.jsonMode() {
		v.options.Progress = a.env.Stderr
	}
	v.interactive = !a.jsonMode() && a.env.InputTerminal && a.env.IsTerminal(a.env.Stderr) && a.env.Stdin != nil
	return v
}

func addLocalHomeFlags(c *cobra.Command, clone bool) {
	c.Flags().Bool("local", false, "use the local Docker daemon; no control plane")
	c.Flags().String("to", "", "name of the new volume to create (default whr-home-<mode>-<container>-<n>)")
	if clone {
		c.Flags().StringArray("exclude", nil, "home-relative path to leave out (repeatable; ./.cache and credentials are always left out)")
		c.Flags().Bool("include-credentials", false, "also copy the credential files a clone leaves out by default")
	}
	c.Flags().Bool("dry-run", false, "preview only; no pull, volume or container")
	c.Flags().String("trust", "", "optional: refuse unless the preview has this exact fingerprint")
	c.Flags().Bool("yes", false, "confirm the previewed copy into a new volume")
}

// LocalHomeCopyHandler is the CLI adapter for workspace home clone and backup.
func LocalHomeCopyHandler(mode string, prepare func(context.Context, localdocker.HomeCopyOptions) (HomeCopyTransaction, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localHomeKey{}).(localHomeInvocation)
		if !v.local || len(call.Args) != 1 || strings.TrimSpace(call.Args[0]) == "" {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "use workspace home %s --local <exact-name|full-id>", mode)
		}
		if !v.dryRun && !v.interactive && !v.yes {
			return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "unattended home %s requires --yes (or --dry-run)", mode)
		}
		v.options.Mode = mode
		v.options.Target = call.Args[0]
		tx, err := prepare(ctx, v.options)
		if err != nil {
			return Result{}, err
		}
		preview := tx.Preview()
		if v.dryRun {
			return Result{Value: preview, Human: homeCopyHuman(preview) + "Apply with --yes, or run without --dry-run to confirm interactively.\n"}, nil
		}
		reader := bufio.NewReaderSize(v.input, 1025)
		if v.interactive {
			if _, err = io.WriteString(v.diagnostics, homeCopyHuman(preview)); err != nil {
				return Result{}, err
			}
		}
		if v.trust == "" {
			v.trust = preview.Fingerprint
		}
		if v.trust != preview.Fingerprint {
			return Result{}, exitcode.Errorf(exitcode.PathConflict, "home copy fingerprint does not match --trust")
		}
		if !v.yes {
			if _, err = io.WriteString(v.diagnostics, "Copy into the new volume with the disclosed excludes? [y/N] "); err != nil {
				return Result{}, err
			}
			answer, e := lifecycleAnswer(ctx, reader)
			if e != nil {
				return Result{}, e
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				return Result{}, exitcode.Errorf(exitcode.PathHumanDecisionRefused, "home %s confirmation declined", mode)
			}
		}
		result, err := tx.Apply(ctx, v.trust)
		res := Result{Value: result, Human: homeCopyHuman(result)}
		if err != nil {
			failure := asError(err)
			copy := *failure
			copy.Result = result
			return res, &copy
		}
		return res, nil
	}
}

func homeCopyHuman(r localdocker.HomeCopyResult) string {
	var b strings.Builder
	if r.Status == "preview" {
		b.WriteString("Pause writers in the source home (agents, editors) for a consistent copy; whr does not stop the workspace.\n")
	}
	fmt.Fprintf(&b, "home %s: %s\n", terminal.Escape(r.Mode), terminal.Escape(r.Status))
	fmt.Fprintf(&b, "source: %s (%s, id=%s) volume %s, mounted read-only\n", terminal.Escape(r.Source.ContainerName), terminal.Escape(r.Source.Role), terminal.Escape(r.Source.ContainerID), terminal.Escape(r.Source.Volume))
	fmt.Fprintf(&b, "target: new volume %s\n", terminal.Escape(r.TargetVolume))
	fmt.Fprintf(&b, "helper image: %s\n", terminal.Escape(r.HelperImage))
	fmt.Fprintf(&b, "excludes: %s\n", terminal.Escape(strings.Join(r.Excludes, " ")))
	credentials := "excluded"
	if r.CredentialsIncluded {
		credentials = "included"
	}
	fmt.Fprintf(&b, "credentials: %s\n", credentials)
	size := "unknown"
	if r.EstimatedSize != nil {
		size = *r.EstimatedSize + " (source volume before excludes)"
	}
	fmt.Fprintf(&b, "estimated size: %s\n", terminal.Escape(size))
	fmt.Fprintf(&b, "fingerprint: %s\n", terminal.Escape(r.Fingerprint))
	for _, a := range r.Actions {
		fmt.Fprintf(&b, "action: %s %s attempted=%t applied=%t\n", terminal.Escape(a.Kind), terminal.Escape(a.Target), a.Attempted, a.Applied)
	}
	if v := r.Verification; v != nil {
		fmt.Fprintf(&b, "verification: %s\n", terminal.Escape(v.Status))
		fmt.Fprintf(&b, "  files source=%d target=%d; directories %d/%d; links %d/%d\n", v.Source.RegularFiles, v.Target.RegularFiles, v.Source.Directories, v.Target.Directories, v.Source.SymbolicLinks, v.Target.SymbolicLinks)
		if v.Source.ClaudeSessionFiles+v.Target.ClaudeSessionFiles+v.Source.CodexSessionFiles+v.Target.CodexSessionFiles > 0 {
			fmt.Fprintf(&b, "  sessions: .claude/projects %d/%d, .codex/sessions %d/%d\n", v.Source.ClaudeSessionFiles, v.Target.ClaudeSessionFiles, v.Source.CodexSessionFiles, v.Target.CodexSessionFiles)
		}
	}
	for _, residual := range r.Residuals {
		fmt.Fprintf(&b, "residual: %s (inspect and remove it yourself if unwanted)\n", terminal.Escape(residual))
	}
	return b.String()
}

func productionHomeCopy(ctx context.Context, o localdocker.HomeCopyOptions) (HomeCopyTransaction, error) {
	h, err := (localdocker.Client{}).PrepareHomeCopy(ctx, doctor.SystemPorts(nil), o)
	if err != nil {
		return nil, err
	}
	return h, nil
}
