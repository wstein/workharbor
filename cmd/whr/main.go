// Command whr is the workharbor CLI and, via `whr serve`, its server.
package main

import (
	"fmt"
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
  help      show this help

Not yet implemented: serve, login, run, ls, show, logs, watch, say, pause,
resume, cancel, purge, usage, inbox, approve, reject, diff, ssh, open, wait,
kill-all, doctor.
`

// editorconfig-checker-enable

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return exitcode.Usage
	}
	switch args[0] {
	case "version", "--version":
		fmt.Println(version.Version)
		return exitcode.OK
	case "help", "-h", "--help":
		fmt.Print(usage)
		return exitcode.OK
	default:
		fmt.Fprintf(os.Stderr, "whr: unknown or unimplemented command %q\n", args[0])
		return exitcode.Usage
	}
}
