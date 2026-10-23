// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localshell"
	"io"
	"os"
)

type localShellKey struct{}
type localShellInvocation struct {
	local, json, inputTerminal, outputTerminal bool
	options                                    localshell.Options
}

func (a *app) localShellOptions(c *cobra.Command) localShellInvocation {
	local, _ := c.Flags().GetBool("local")
	out, _ := a.env.Stdout.(*os.File)
	diag, _ := a.env.Stderr.(*os.File)
	return localShellInvocation{local: local, json: a.jsonMode(), inputTerminal: a.env.InputTerminal, outputTerminal: a.env.IsTerminal(a.env.Stdout), options: localshell.Options{Term: a.env.Getenv("TERM"), Stderr: a.env.Stderr, StderrTerminal: a.env.IsTerminal(a.env.Stderr), Streams: localshell.Streams{Stdin: a.env.TerminalInput, Stdout: out, Stderr: diag}}}
}

// LocalShellHandler admits terminal-only local usage before any Docker boundary.
// All remote variants retain their owner_api refusal and events contract.
func LocalShellHandler(p localshell.Ports) Handler {
	return func(ctx context.Context, call Call) (Result, error) {
		v, _ := ctx.Value(localShellKey{}).(localShellInvocation)
		if !v.local {
			return Result{}, exitcode.Errorf(exitcode.PathUnavailableCommand, "workspace.shell is not available yet: it waits for %s (owner_api); use --local for a local shell", WaitsFor("workspace.shell"))
		}
		if v.json {
			return Result{}, exitcode.Errorf(exitcode.PathUnsupportedJSON, "shell --local does not support --json")
		}
		if len(call.Args) != 1 {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "shell --local requires exactly one workspace selector and takes no trailing command")
		}
		if !v.inputTerminal || !v.outputTerminal || v.options.Streams.Stdin == nil || v.options.Streams.Stdout == nil {
			return Result{}, exitcode.Errorf(exitcode.PathBadArguments, "shell --local requires terminal standard input and output")
		}
		if v.options.Stderr == nil {
			v.options.Stderr = io.Discard
		}
		status, err := localshell.Run(ctx, call.Args[0], v.options, p)
		if err != nil {
			return Result{}, err
		}
		return Result{terminalStatus: &status}, nil
	}
}
