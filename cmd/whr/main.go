// Command whr is the workharbor CLI and, via `whr serve`, its server.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/version"
)

// editorconfig-checker-disable
const usage = `whr - workharbor agent work supervisor

Usage:
  whr <command> [arguments]

Commands:
  version   print version
  tools     build the shared tool store (whr tools build)
  help      show this help

Not yet implemented: serve, login, run, ls, show, logs, watch, say, pause,
resume, cancel, purge, usage, inbox, approve, reject, diff, ssh, open, wait,
kill-all, doctor.
`

// editorconfig-checker-enable

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command. Stdout is data, stderr is human text (design §9.2).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitcode.Usage
	}
	switch args[0] {
	case "version", "--version":
		return runVersion(args[1:], stdout, stderr)
	case "tools":
		return runTools(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitcode.OK
	default:
		fmt.Fprintf(stderr, "whr: unknown or unimplemented command %q\n", args[0])
		return exitcode.Usage
	}
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
