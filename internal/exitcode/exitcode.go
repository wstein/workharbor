// Package exitcode defines the stable whr exit codes. See docs/design.md §9.2.
package exitcode

import "errors"

const (
	OK         = 0
	Error      = 1
	Usage      = 2
	NotFound   = 3
	Auth       = 4
	Conflict   = 5
	NeedsHuman = 6
	Timeout    = 7
	TaskFailed = 10
)

// Coder is implemented by errors that know the exit code they map to.
type Coder interface {
	ExitCode() int
}

// From returns the exit code for an error: OK for nil, the code of the first
// error in the chain that implements Coder, and Error for any other error.
func From(err error) int {
	if err == nil {
		return OK
	}
	var c Coder
	if errors.As(err, &c) {
		return c.ExitCode()
	}
	return Error
}
