// SPDX-License-Identifier: EUPL-1.2

// Package exitcode holds the whr exit-code table of ADR 0013, mirrored by
// contracts/cli/exit-codes.json and frozen against it by table_test.go.
package exitcode

import (
	"context"
	"errors"
	"fmt"
)

// Code is a process exit status chosen by whr itself.
type Code int

// The CLI-owned codes (ADR 0013). 75-79 stay reserved and unassigned.
const (
	OK                   Code = 0
	Usage                Code = 64
	NotFound             Code = 65
	Unauthenticated      Code = 66
	Forbidden            Code = 67
	Conflict             Code = 68
	IncompatibleContract Code = 69
	NeedsHuman           Code = 70
	Timeout              Code = 71
	Interrupted          Code = 72
	PartialFailure       Code = 73
	Internal             Code = 74
)

// RemoteMin and RemoteMax bound the remote statuses that shell, console, ssh
// and agent attach pass through unchanged (FR-019).
const (
	RemoteMin = 0
	RemoteMax = 255
)

// Entry is one row of the table.
type Entry struct {
	Code     int
	Name     string
	Category string
}

var table = []Entry{
	{0, "ok", "success"},
	{64, "usage", "usage"},
	{65, "not_found", "not-found"},
	{66, "unauthenticated", "authentication"},
	{67, "forbidden", "authorization"},
	{68, "conflict", "conflict-or-wrong-state"},
	{69, "incompatible_contract", "incompatible-contract-version"},
	{70, "needs_human", "needs-human"},
	{71, "timeout", "timeout"},
	{72, "interrupted", "interrupted"},
	{73, "partial_failure", "partial-failure"},
	{74, "internal", "unclassified-failure"},
}

// Table returns a copy of the exit-code table.
func Table() []Entry { return append([]Entry(nil), table...) }

// Name returns the table name of code, which is also the --json error.code.
func Name(code int) (string, bool) {
	for _, e := range table {
		if e.Code == code {
			return e.Name, true
		}
	}
	return "", false
}

// Path names a way a command can end; every path maps to exactly one code.
type Path string

// The failure paths that exist in the first increment plus those the table
// already fixes for later commands (SC-012 stays open until T017).
const (
	PathBadArguments           Path = "bad-arguments"
	PathUnsupportedJSON        Path = "unsupported-json"
	PathUnavailableCommand     Path = "unavailable-command"
	PathUnknownCommand         Path = "unknown-command"
	PathTargetAbsent           Path = "target-absent"
	PathSessionInterfaceAbsent Path = "session-interface-absent"
	PathNoCredential           Path = "no-credential"
	PathNotAuthorized          Path = "not-authorized"
	PathConflict               Path = "conflict"
	PathWrongState             Path = "wrong-state"
	PathIdempotencyMismatch    Path = "idempotency-mismatch"
	PathIncompatibleVersion    Path = "incompatible-version"
	PathHumanDecisionRefused   Path = "human-decision-refused"
	PathHumanDecisionMissing   Path = "human-decision-missing"
	PathDeadlineExceeded       Path = "deadline-exceeded"
	PathUserInterrupt          Path = "user-interrupt"
	PathTransportLost          Path = "transport-lost"
	PathPartialFailure         Path = "partial-failure"
	PathUnclassified           Path = "unclassified"
)

var pathCodes = map[Path]Code{
	PathBadArguments: Usage, PathUnsupportedJSON: Usage, PathUnavailableCommand: Usage, PathUnknownCommand: Usage,
	PathTargetAbsent: NotFound, PathSessionInterfaceAbsent: NotFound,
	PathNoCredential: Unauthenticated, PathNotAuthorized: Forbidden,
	PathConflict: Conflict, PathWrongState: Conflict, PathIdempotencyMismatch: Conflict,
	PathIncompatibleVersion:  IncompatibleContract,
	PathHumanDecisionRefused: NeedsHuman, PathHumanDecisionMissing: NeedsHuman,
	PathDeadlineExceeded: Timeout,
	PathUserInterrupt:    Interrupted, PathTransportLost: Interrupted,
	PathPartialFailure: PartialFailure,
	PathUnclassified:   Internal,
}

// Code returns the exit code of the path; an undeclared path is Internal.
func (p Path) Code() Code {
	if c, ok := pathCodes[p]; ok {
		return c
	}
	return Internal
}

// Paths lists every declared path.
func Paths() []Path {
	out := make([]Path, 0, len(pathCodes))
	for p := range pathCodes {
		out = append(out, p)
	}
	return out
}

// Error is a CLI-owned failure. Message is one line without the "whr:" prefix;
// Remediation is the optional second line.
type Error struct {
	Path        Path
	Message     string
	Remediation string
	// Result is optional command data that accompanies the failure, such as
	// the findings of a failed validate (carried in the --json envelope).
	Result any
}

func (e *Error) Error() string { return e.Message }

// Code returns the exit code of the error's path.
func (e *Error) Code() Code { return e.Path.Code() }

// Errorf creates an Error for path.
func Errorf(path Path, format string, args ...any) *Error {
	return &Error{Path: path, Message: fmt.Sprintf(format, args...)}
}

// WithRemediation returns e with its remediation line set.
func (e *Error) WithRemediation(format string, args ...any) *Error {
	e.Remediation = fmt.Sprintf(format, args...)
	return e
}

// FromError maps err to a code: nil is OK, an Error uses its path, deadline
// and cancellation use Timeout and Interrupted, anything else is Internal.
func FromError(err error) Code {
	var e *Error
	switch {
	case err == nil:
		return OK
	case errors.As(err, &e):
		return e.Code()
	case errors.Is(err, context.DeadlineExceeded):
		return Timeout
	case errors.Is(err, context.Canceled):
		return Interrupted
	}
	return Internal
}

// Kind tells a CLI-owned status from a remote one in the --json exit event.
type Kind string

// The two kinds of exit status.
const (
	KindCLI    Kind = "cli"
	KindRemote Kind = "remote"
)

// Status is an exit status together with its origin.
type Status struct {
	Code int
	Kind Kind
}

// CLI wraps a CLI-owned code.
func CLI(code Code) Status { return Status{Code: int(code), Kind: KindCLI} }

// Remote passes a remote status 0-255 through unchanged and unmapped.
func Remote(status int) (Status, error) {
	if status < RemoteMin || status > RemoteMax {
		return Status{}, fmt.Errorf("remote status %d outside %d-%d", status, RemoteMin, RemoteMax)
	}
	return Status{Code: status, Kind: KindRemote}, nil
}

// TransportLost is the status when an interactive transport drops.
func TransportLost() Status { return CLI(Interrupted) }
