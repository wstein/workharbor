// SPDX-License-Identifier: EUPL-1.2

package sandbox

import "github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"

// State is a lifecycle state of an instance (FR-004).
type State string

// The lifecycle states. StateNone is the state before creation.
const (
	StateNone              State = ""
	StatePreparing         State = "preparing"
	StateReady             State = "ready"
	StateRunning           State = "running"
	StateFinished          State = "finished"
	StateFailed            State = "failed"
	StateDiscarding        State = "discarding"
	StateDiscarded         State = "discarded"
	StateCleanupIncomplete State = "cleanup_incomplete"
)

// States lists StateNone and the eight lifecycle states.
func States() []State {
	return []State{StateNone, StatePreparing, StateReady, StateRunning, StateFinished, StateFailed,
		StateDiscarding, StateDiscarded, StateCleanupIncomplete}
}

// transitions is the table of specs/008-workspace-sandbox/data-model.md. Any
// pair not listed is refused.
var transitions = map[State][]State{
	StateNone:              {StatePreparing},
	StatePreparing:         {StateReady, StateFailed},
	StateReady:             {StateRunning, StateDiscarding},
	StateRunning:           {StateFinished, StateFailed},
	StateFinished:          {StateDiscarding},
	StateFailed:            {StateDiscarding},
	StateDiscarding:        {StateDiscarded, StateCleanupIncomplete},
	StateCleanupIncomplete: {StateDiscarding},
}

// Transition returns the new state, or the old state and refused.invalid_state
// when the move is not in the table. It changes nothing else.
func Transition(from, to State) (State, error) {
	for _, next := range transitions[from] {
		if next == to {
			return to, nil
		}
	}
	return from, runtime.NewRefusal(runtime.RefusedInvalidState)
}

// CanLeave reports whether any transition starts at s.
func (s State) CanLeave() bool { return len(transitions[s]) > 0 }

// ExportAllowed reports whether results may be exported: a finished instance,
// or a failed one, in which case the manifest states partial=true (FR-007).
func (s State) ExportAllowed() bool { return s == StateFinished || s == StateFailed }
