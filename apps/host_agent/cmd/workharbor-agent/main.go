// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"context"
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/connection"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 3 && args[0] == "run" && args[1] == "--identity-dir" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if connection.Run(ctx, args[2], nil) != nil {
			fmt.Fprintln(errOut, "workharbor-agent: host_transport_unavailable")
			return 1
		}
		return 0
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		fmt.Fprintln(out, "WorkHarbor host agent.\nUsage: workharbor-agent [--help | --version | run --identity-dir PATH]")
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(out, "workharbor-agent 0.1.0-dev (scaffold)")
		return 0
	}
	fmt.Fprintln(errOut, "workharbor-agent: product commands are not implemented; use --help")
	return 1
}
