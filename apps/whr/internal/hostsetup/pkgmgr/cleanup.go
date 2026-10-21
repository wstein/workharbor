// SPDX-License-Identifier: EUPL-1.2
package pkgmgr

// CleanupError retains ownership of a started process whose cleanup failed.
// Callers must stop further mutation and preserve this error until explicit
// reconciliation. Its process is not proof of successful cleanup or application;
// after terminal ownership release it must never signal a reused numeric PID.
// This is in-memory ownership, not a durable recovery receipt: serialization or
// logging must not replace retaining and reconciling the actual error/handle.
type CleanupError interface {
	error
	CleanupProcess() Process
	// StopRun marks the error for plan.RunStopper: orchestration runs no
	// further step and returns the error to its caller unchanged.
	StopRun()
}
type processCleanupError struct {
	process Process
	cause   error
}

func (e *processCleanupError) Error() string           { return "package process cleanup failed" }
func (e *processCleanupError) Unwrap() error           { return e.cause }
func (e *processCleanupError) CleanupProcess() Process { return e.process }
func (e *processCleanupError) StopRun()                {}

// NewCleanupError binds a cleanup failure to its still-accounted process. It
// retains the underlying cause for errors.Is without exposing command output.
func NewCleanupError(process Process, cause error) error {
	if cause == nil {
		return nil
	}
	return &processCleanupError{process: process, cause: cause}
}
