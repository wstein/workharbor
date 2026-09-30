// Package exitcode defines the stable whr exit codes. See docs/design.md §9.2.
package exitcode

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
