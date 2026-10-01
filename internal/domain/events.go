package domain

import (
	"encoding/json"
	"time"
)

// Tier says how long an event is kept (design §5.4): audit entries are never
// purged, transcript content has retention.
type Tier string

const (
	TierAudit      Tier = "audit"
	TierTranscript Tier = "transcript"
)

// EventKind names what happened.
type EventKind string

const (
	EventTaskState          EventKind = "task.state"
	EventRunStarted         EventKind = "run.started"
	EventRunState           EventKind = "run.state"
	EventEnvState           EventKind = "env.state"
	EventRevisionPinned     EventKind = "revision.pinned"
	EventCIRecorded         EventKind = "ci.recorded"
	EventDecisionRaised     EventKind = "decision.raised"
	EventDecisionAnswered   EventKind = "decision.answered"
	EventDecisionExpired    EventKind = "decision.expired"
	EventDecisionSuperseded EventKind = "decision.superseded"
	EventDecisionReraised   EventKind = "decision.reraised"
	EventPurged             EventKind = "store.purged"
)

// Event is an append-only record: the audit trail, the UI feed and the CLI
// stream. Domain methods record the events a change produced and the caller
// takes them with TakeEvents; the store assigns Seq and, when At is zero,
// stamps the time.
type Event struct {
	Seq     int64 // assigned by the store: monotonic and never reused
	TaskID  ID
	Kind    EventKind
	Tier    Tier
	Payload []byte // JSON
	At      time.Time
}

// StateChanged is the payload of a state change of a task, run, environment or
// Decision.
type StateChanged struct {
	Object string `json:"object"`
	ID     ID     `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
}

// RunStarted is the payload of EventRunStarted.
type RunStarted struct {
	RunID ID `json:"run_id"`
	EnvID ID `json:"env_id"`
}

// RevisionPinned is the payload of EventRevisionPinned.
type RevisionPinned struct {
	RunID  ID     `json:"run_id"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

// CIRecorded is the payload of EventCIRecorded.
type CIRecorded struct {
	SHA   string  `json:"sha"`
	State CIState `json:"state"`
}

// DecisionRaised is the payload of EventDecisionRaised. For an approval it
// carries the tool and a capped copy of its input, as design §5.4 requires of
// the audit trail.
type DecisionRaised struct {
	ID       ID           `json:"id"`
	RunID    ID           `json:"run_id,omitempty"`
	Kind     DecisionKind `json:"kind"`
	Blocking bool         `json:"blocking"`
	Subject  string       `json:"subject,omitempty"`
	Input    string       `json:"input,omitempty"`
	SHA      string       `json:"sha,omitempty"`
	Deadline time.Time    `json:"deadline,omitzero"`
}

// Reraised is the payload of EventDecisionReraised.
type Reraised struct {
	ID    ID `json:"id"`
	NewID ID `json:"new_id"`
}

// AnswerRecorded is the payload of EventDecisionAnswered.
type AnswerRecorded struct {
	ID     ID        `json:"id"`
	Answer string    `json:"answer"`
	By     string    `json:"by"`
	Reason string    `json:"reason,omitempty"`
	SHA    string    `json:"sha,omitempty"`
	At     time.Time `json:"at"`
}

// newEvent returns an audit event with a JSON payload. The payload types are
// plain structs, so marshalling cannot fail.
func newEvent(task ID, kind EventKind, payload any, at time.Time) Event {
	b, err := json.Marshal(payload)
	if err != nil {
		panic("domain: event payload: " + err.Error())
	}
	return Event{TaskID: task, Kind: kind, Tier: TierAudit, Payload: b, At: at}
}
