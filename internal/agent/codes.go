package agent

import (
	"errors"
	"fmt"
)

// codes map every sentinel error to the string that crosses the plugin seam
// (design §5.5). The order is the order Code tries them in.
var codes = []struct {
	code string
	err  error
}{
	{"unsupported_auth", ErrUnsupportedAuth},
	{"unsupported", ErrUnsupported},
	{"no_approver", ErrNoApprover},
	{"no_session", ErrNoSession},
	{"not_running", ErrNotRunning},
	{"bad_spec", ErrBadSpec},
}

// CodeUnknown is the code of an error that is not one of the contract's.
const CodeUnknown = "error"

// Code returns the string code of an error for the wire: the code of the
// first contract error in its chain, "" for nil and CodeUnknown for any other.
func Code(err error) string {
	if err == nil {
		return ""
	}
	for _, c := range codes {
		if errors.Is(err, c.err) {
			return c.code
		}
	}
	return CodeUnknown
}

// ErrorFor is the inverse of Code, for the supervisor's side of the seam: it
// returns an error that matches the contract's sentinel for the code with
// errors.Is, and carries the plugin's message as detail. An unknown code gives
// a plain error.
func ErrorFor(code, detail string) error {
	for _, c := range codes {
		if c.code == code {
			if detail == "" {
				return c.err
			}
			return fmt.Errorf("%w: %s", c.err, detail)
		}
	}
	if detail == "" {
		detail = code
	}
	return errors.New(detail)
}
