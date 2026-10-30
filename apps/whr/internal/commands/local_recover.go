// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// RecoverSession is the prepared recovery container bound to one daemon.
type RecoverSession interface {
	Preview() localdocker.RecoverResult
	Start(ctx context.Context, s localdocker.RecoverStreams) (localdocker.RecoverResult, error)
}

type localRecoverKey struct{}
type localRecoverInvocation struct {
	local, dryRun, toolboxSet, inputTerminal, outputTerminal bool
	options                                                  localdocker.RecoverOptions
	input                                                    io.Reader
	diagnostics                                              io.Writer
	streams                                                  localdocker.RecoverStreams
}

func (a *app) localRecoverOptions(c *cobra.Command) localRecoverInvocation {
	v := localRecoverInvocation{input: a.env.Stdin, diagnostics: a.env.Stderr, inputTerminal: a.env.InputTerminal, outputTerminal: a.env.IsTerminal(a.env.Stdout)}
	v.local, _ = c.Flags().GetBool("local")
	v.dryRun, _ = c.Flags().GetBool("dry-run")
	v.options.Toolbox, _ = c.Flags().GetString("toolbox")
	v.options.Nixbox, _ = c.Flags().GetString("nixbox")
	v.toolboxSet = c.Flags().Changed("toolbox")
	v.options.RW, _ = c.Flags().GetBool("rw")
	v.options.Net, _ = c.Flags().GetBool("net")
	v.options.Term = a.env.Getenv("TERM")
	out, _ := a.env.Stdout.(*os.File)
	diag, _ := a.env.Stderr.(*os.File)
	v.streams = localdocker.RecoverStreams{Stdin: a.env.TerminalInput, Stdout: out, Stderr: diag}
	return v
}

func addLocalRecoverFlags(c *cobra.Command) {
	c.Flags().Bool("local", false, "use the local Docker daemon; no control plane")
	c.Flags().String("toolbox", "", "toolbox to mount: latest (default), a tag, a sha256 digest, current (the workspace's own) or a local volume id")
	c.Flags().String("nixbox", "", "nixbox to mount at /nix: latest (default), a tag, a sha256 digest, current or a local volume id")
	c.Flags().Bool("rw", false, "mount the home read-write (default read-only)")
	c.Flags().Bool("net", false, "allow network access (default none)")
	c.Flags().Bool("dry-run", false, "preview only; no pull, volume or container")
}

// LocalRecoverHandler is the CLI adapter for workspace recover.
func LocalRecoverHandler(prepare func(context.Context, localdocker.RecoverOptions) (RecoverSession, error)) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localRecoverKey{}).(localRecoverInvocation)
		if !v.local || len(call.Args) != 1 || strings.TrimSpace(call.Args[0]) == "" {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "use workspace recover --local <exact-name|full-id>")
		}
		v.options.Target = call.Args[0]
		if !v.dryRun && (!v.inputTerminal || !v.outputTerminal || v.streams.Stdin == nil || v.streams.Stdout == nil) {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "recover --local requires terminal standard input and output (or --dry-run)")
		}
		if !v.dryRun && !v.toolboxSet {
			if _, err := io.WriteString(v.diagnostics, "Toolbox: latest, current (the workspace's own), a tag, or a local volume id [latest]: "); err != nil {
				return Result{}, err
			}
			answer, err := lifecycleAnswer(ctx, bufio.NewReaderSize(v.input, 1025))
			if err != nil {
				return Result{}, err
			}
			if answer = strings.TrimSpace(answer); answer != "" && answer != "latest" {
				v.options.Toolbox = answer
			}
		}
		session, err := prepare(ctx, v.options)
		if err != nil {
			return Result{}, err
		}
		preview := session.Preview()
		if v.dryRun {
			return Result{Value: preview, Human: recoverHuman(preview)}, nil
		}
		if _, err = io.WriteString(v.diagnostics, recoverHuman(preview)+"Leave with exit; the container is removed afterwards.\n"); err != nil {
			return Result{}, err
		}
		if v.streams.Stderr == nil {
			v.streams.Stderr = v.streams.Stdout
		}
		result, err := session.Start(ctx, v.streams)
		if err != nil {
			failure := asError(err)
			copy := *failure
			copy.Result = result
			return Result{Value: result, Human: recoverHuman(result)}, &copy
		}
		status := result.ExitStatus
		return Result{terminalStatus: &status}, nil
	}
}

func recoverHuman(r localdocker.RecoverResult) string {
	var b strings.Builder
	access := "read-only"
	if !r.ReadOnly {
		access = "READ-WRITE"
	}
	network := "off"
	if r.Network {
		network = "on"
	}
	fmt.Fprintf(&b, "recover: %s home %s (%s), toolbox %s, nixbox %s, network %s\n", terminal.Escape(r.Source.ContainerName), terminal.Escape(r.Source.Volume), access, terminal.Escape(r.Toolbox), terminal.Escape(r.Nixbox), network)
	fmt.Fprintf(&b, "image: %s (stock, pulled when missing; no package manager runs)\n", terminal.Escape(r.Image))
	for _, a := range r.Actions {
		fmt.Fprintf(&b, "action: %s %s attempted=%t applied=%t\n", terminal.Escape(a.Kind), terminal.Escape(a.Target), a.Attempted, a.Applied)
	}
	for _, residual := range r.Residuals {
		fmt.Fprintf(&b, "residual: %s (inspect and remove it yourself if unwanted)\n", terminal.Escape(residual))
	}
	return b.String()
}

func productionRecover(ctx context.Context, o localdocker.RecoverOptions) (RecoverSession, error) {
	r, err := (localdocker.Client{}).PrepareRecover(ctx, doctor.SystemPorts(nil), o)
	if err != nil {
		return nil, err
	}
	return r, nil
}
