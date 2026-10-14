// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"fmt"
	"io"
	"os"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		fmt.Fprintln(out, "whr scaffold: product commands are not implemented.\nUsage: whr [--help | --version]")
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(out, "whr 0.1.0-dev (scaffold)")
		return 0
	}
	fmt.Fprintln(errOut, "whr: product commands are not implemented; use --help")
	return 64
}
