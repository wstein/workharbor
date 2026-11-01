// SPDX-License-Identifier: EUPL-1.2

package sandbox

import (
	"fmt"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
)

// Retention is independent of state (FR-006).
type Retention string

// The two retentions.
const (
	RetentionEphemeral Retention = "ephemeral"
	RetentionKept      Retention = "kept"
)

// Valid reports whether r is one of the two retentions.
func (r Retention) Valid() bool { return r == RetentionEphemeral || r == RetentionKept }

// ResultsState says what the instance holds that a discard would delete.
type ResultsState string

// The results states. Anything else is treated as unexported.
const (
	ResultsNone       ResultsState = "none"
	ResultsUnexported ResultsState = "unexported"
	ResultsExported   ResultsState = "exported"
)

func (r ResultsState) safeToDelete() bool { return r == ResultsNone || r == ResultsExported }

// Record is the part of an ExecutionInstance that lifecycle rules read.
type Record struct {
	ID        ID
	Name      string
	Workspace string
	Profile   runtime.TrustProfile
	Provider  string
	State     State
	Retention Retention
	Results   ResultsState
}

// Lookup finds a record by id, or ErrNotFound.
func Lookup(records []Record, id ID) (Record, error) {
	for _, r := range records {
		if r.ID == id {
			return r, nil
		}
	}
	return Record{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Keep returns the record retained for review. It implies no merge, push,
// publication or acceptance and changes nothing but the retention; it is
// idempotent and refused once the instance is being discarded.
func Keep(r Record) (Record, error) {
	switch r.State {
	case StateNone, StateDiscarding, StateDiscarded, StateCleanupIncomplete:
		return r, runtime.NewRefusal(runtime.RefusedInvalidState)
	}
	r.Retention = RetentionKept
	return r, nil
}

// DiscardOptions carries the operator's confirmation. Unattended is set when
// no human can be asked, for example in an answer-file run.
type DiscardOptions struct {
	ConfirmResultsDeletion bool
	Unattended             bool
}

// CheckDiscard decides whether a discard may start (FR-008). The state must
// allow the move to discarding (cleanup_incomplete retries); kept does not
// block a discard. Results not exported to an explicit destination refuse with
// refused.unexported_results unless deletion is confirmed; unattended without
// confirmation is ErrConfirmationRequired (needs-human) instead.
func CheckDiscard(r Record, o DiscardOptions) error {
	if _, err := Transition(r.State, StateDiscarding); err != nil {
		return err
	}
	if r.Results.safeToDelete() || o.ConfirmResultsDeletion {
		return nil
	}
	if o.Unattended {
		return fmt.Errorf("%w: discarding %s deletes unexported results", ErrConfirmationRequired, r.ID)
	}
	return runtime.NewRefusal(runtime.RefusedUnexportedResults)
}

// Cleanable reports whether clean may remove the record: finished or failed,
// ephemeral and without unexported results. A kept instance is never cleaned.
func Cleanable(r Record) bool {
	return (r.State == StateFinished || r.State == StateFailed) &&
		r.Retention == RetentionEphemeral && r.Results.safeToDelete()
}

// CleanCandidates returns the cleanable records in their given order.
func CleanCandidates(records []Record) []Record {
	var out []Record
	for _, r := range records {
		if Cleanable(r) {
			out = append(out, r)
		}
	}
	return out
}
