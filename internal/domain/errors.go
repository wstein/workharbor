package domain

import (
	"errors"
	"fmt"

	"github.com/wstein/workharbor/internal/exitcode"
)

// Rule names the rule a ConflictError broke, so callers and tests can tell
// violations apart without parsing messages.
type Rule string

// RuleTransition is an illegal state transition (design §4.1).
const RuleTransition Rule = "transition"

// ConflictError reports a change that breaks a rule: an illegal state
// transition or a coupling rule between task, run and environment (design
// §4.1). It maps to exit code Conflict.
type ConflictError struct {
	Rule Rule
	Msg  string
}

func conflict(rule Rule, format string, args ...any) *ConflictError {
	return &ConflictError{Rule: rule, Msg: fmt.Sprintf(format, args...)}
}

// NewConflict returns a conflict error for another package to report, such as
// the store when a compare-and-swap loses.
func NewConflict(rule Rule, format string, args ...any) *ConflictError {
	return conflict(rule, format, args...)
}

func (e *ConflictError) Error() string { return e.Msg }

// ExitCode implements exitcode.Coder.
func (e *ConflictError) ExitCode() int { return exitcode.Conflict }

// NotFoundError reports an unknown run, environment or commit. It maps to
// exit code NotFound.
type NotFoundError struct {
	Kind string
	ID   string
}

// ErrNotFound matches every *NotFoundError with errors.Is.
var ErrNotFound = errors.New("not found")

// Is makes errors.Is(err, ErrNotFound) true.
func (e *NotFoundError) Is(target error) bool { return target == ErrNotFound }

func (e *NotFoundError) Error() string { return fmt.Sprintf("%s %s not found", e.Kind, e.ID) }

// ExitCode implements exitcode.Coder.
func (e *NotFoundError) ExitCode() int { return exitcode.NotFound }
