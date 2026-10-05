// Command whr is the workharbor CLI and, via `whr serve`, its server.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/cli"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/version"
)

func main() {
	ctx, stop, shellSignals := commandSignalContext()
	defer stop()
	code := executeWithShellSignals(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, shellSignals)
	stop()
	os.Exit(code)
}

// run executes one command with a background context. Stdout is data, stderr is
// human text (design §9.2).
func run(args []string, stdout, stderr io.Writer) int {
	return execute(context.Background(), args, os.Stdin, stdout, stderr)
}

func execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return executeWithShellSignals(ctx, args, stdin, stdout, stderr, nil)
}

// commandSignalContext keeps ordinary command interruption while allowing the
// sign-in shell's child to receive terminal interrupts without losing its hold.
func commandSignalContext() (context.Context, context.CancelFunc, func() func()) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	var childActive atomic.Bool
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-signals:
				if sig == syscall.SIGINT && childActive.Load() {
					continue
				}
				cancel()
			}
		}
	}()
	stop := func() { signal.Stop(signals); cancel() }
	scope := func() func() {
		previous := childActive.Swap(true)
		return func() { childActive.Store(previous) }
	}
	return ctx, stop, scope
}

func executeWithShellSignals(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, shellSignals func() func()) int {
	if len(args) > 0 && args[0] == "--version" {
		args = append([]string{"version"}, args[1:]...)
	}
	return cli.Execute(ctx, cli.Env{
		Stdin: stdin, Stdout: stdout, Stderr: stderr, Getenv: os.Getenv, ShellSignals: shellSignals,
		Extra: []*cobra.Command{versionCommand(stdout, stderr), toolsCommand(stdout, stderr), serveCommand(stderr)},
	}, args)
}

// passthrough builds a command that hands its arguments to a function that
// parses them itself, as `whr version` and `whr tools build` always have.
func passthrough(use, short string, fn func(args []string) int) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if code := fn(args); code != exitcode.OK {
				return exitCodeError(code)
			}
			return nil
		},
	}
}

// exitCodeError carries the exit code of a command that has already said what
// went wrong on stderr.
type exitCodeError int

func (e exitCodeError) Error() string { return "" }
func (e exitCodeError) ExitCode() int { return int(e) }

func versionCommand(stdout, stderr io.Writer) *cobra.Command {
	return passthrough("version", "Print the version", func(args []string) int { return runVersion(args, stdout, stderr) })
}

func toolsCommand(stdout, stderr io.Writer) *cobra.Command {
	return passthrough("tools", "Build the shared tool store", func(args []string) int { return runTools(args, stdout, stderr) })
}

// runVersion prints the version, the commit and whether the tree was dirty;
// --json gives the same as data.
func runVersion(args []string, stdout, stderr io.Writer) int {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			fmt.Fprintf(stderr, "whr version: unknown option %q\n", a)
			return exitcode.Usage
		}
		asJSON = true
	}
	info := version.Get()
	if asJSON {
		b, err := info.JSON()
		if err != nil {
			fmt.Fprintf(stderr, "whr version: %v\n", err)
			return exitcode.Error
		}
		fmt.Fprintf(stdout, "%s\n", b)
		return exitcode.OK
	}
	fmt.Fprintf(stdout, "%s\n", info.Text())
	return exitcode.OK
}
