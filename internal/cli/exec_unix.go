//go:build unix

package cli

import (
	"regexp"
	"syscall"
)

// termName is a TERM value worth passing on: a terminfo name. Anything else is
// left out, because the value comes from the caller's environment.
var termName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,39}$`)

// execReplace replaces this process with bin. It returns only on failure. Go opens
// its own descriptors close-on-exec, so the new program gets the terminal's three
// and nothing else.
func execReplace(bin string, argv, env []string) error {
	return syscall.Exec(bin, argv, env) //nolint:gosec // the container CLI found by LookPath, with arguments built from checked values
}
