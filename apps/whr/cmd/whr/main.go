// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is the thin entry point: all behavior lives in internal/commands.
func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	return commands.Execute(ctx, args, commands.Env{
		Stdout: out, Stderr: errOut, Getenv: os.Getenv, IsTerminal: isTerminal,
	}, commands.DefaultHandlers())
}

// isTerminal reports whether w is a character device other than /dev/null.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}
