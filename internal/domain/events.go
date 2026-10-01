package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Tier says how long an event is kept (design §5.4): audit entries are never
// purged, transcript content has retention.
type Tier string

const (
	TierAudit      Tier = "audit"
	TierTranscript Tier = "transcript"
	// TierEphemeral is for events that are published to subscribers and never
	// stored: token deltas and heartbeats (design §5.4). The store refuses it.
	TierEphemeral Tier = "ephemeral"
)

// EventKind names what happened.
type EventKind string

const (
	EventTaskState          EventKind = "task.state"
	EventRunStarted         EventKind = "run.started"
	EventRunState           EventKind = "run.state"
	EventRunSession         EventKind = "run.session"
	EventEnvState           EventKind = "env.state"
	EventEnvAdded           EventKind = "env.added"
	EventRunAttempt         EventKind = "run.attempt"
	EventRevisionPinned     EventKind = "revision.pinned"
	EventCIRecorded         EventKind = "ci.recorded"
	EventPRRecorded         EventKind = "pr.recorded"
	EventRevisionPushed     EventKind = "revision.pushed"
	EventDecisionRaised     EventKind = "decision.raised"
	EventDecisionAnswered   EventKind = "decision.answered"
	EventDecisionExpired    EventKind = "decision.expired"
	EventDecisionSuperseded EventKind = "decision.superseded"
	EventDecisionReraised   EventKind = "decision.reraised"
	EventPurged             EventKind = "store.purged"
	EventWorkspaceAdded     EventKind = "workspace.added"
	EventWorkspaceRemoved   EventKind = "workspace.removed"
	EventAgentAdded         EventKind = "agent.added"
	EventAgentRemoved       EventKind = "agent.removed"
	// EventInstruction is a message the human sent to a run, with how it was
	// delivered (design §5.3, Say).
	EventInstruction EventKind = "instruction.sent"
	// EventTranscript is one observation of the agent (a message, a tool call or
	// result, a diff, a test result, usage), stored in the transcript tier.
	EventTranscript EventKind = "transcript"
	// EventTaskHeld records why a task waits for a human before its run starts.
	EventTaskHeld EventKind = "task.held"
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

// EnvAdded is the payload of EventEnvAdded.
type EnvAdded struct {
	ID      ID     `json:"id"`
	Backend string `json:"backend"`
	State   string `json:"state"`
}

// RunAttempt is the payload of EventRunAttempt: a launch of the agent that
// failed, and how many attempts the run has used.
type RunAttempt struct {
	RunID    ID  `json:"run_id"`
	Attempts int `json:"attempts"`
}

// RunSession is the payload of EventRunSession.
type RunSession struct {
	RunID     ID     `json:"run_id"`
	SessionID string `json:"session_id"`
}

// RevisionPinned is the payload of EventRevisionPinned.
type RevisionPinned struct {
	RunID  ID     `json:"run_id"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
	Source string `json:"source,omitempty"`
}

// RevisionPushed is the payload of EventRevisionPushed.
type RevisionPushed struct {
	SHA string `json:"sha"`
}

// CIRecorded is the payload of EventCIRecorded.
type CIRecorded struct {
	SHA   string  `json:"sha"`
	State CIState `json:"state"`
}

// PRRecorded is the payload of EventPRRecorded.
type PRRecorded struct {
	SHA string `json:"sha"`
	URL string `json:"url"`
}

// DecisionRaised is the payload of EventDecisionRaised. For an approval it
// carries the tool and a capped copy of its input, as design §5.4 requires of
// the audit trail.
type DecisionRaised struct {
	ID       ID            `json:"id"`
	RunID    ID            `json:"run_id,omitempty"`
	Kind     DecisionKind  `json:"kind"`
	Blocking bool          `json:"blocking"`
	Subject  string        `json:"subject,omitempty"`
	Input    string        `json:"input,omitempty"`
	SHA      string        `json:"sha,omitempty"`
	Deadline time.Time     `json:"deadline,omitzero"`
	Cause    DecisionCause `json:"cause,omitempty"`
	ResumeAt time.Time     `json:"resume_at,omitzero"`
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

// InstructionSent is the payload of EventInstruction.
type InstructionSent struct {
	RunID    ID     `json:"run_id"`
	Delivery string `json:"delivery"` // injected, next_turn or resumed_turn
	Text     string `json:"text"`
}

// NewInstructionEvent returns the audit event of a message sent to a run.
func NewInstructionEvent(task, run ID, delivery, text string, at time.Time) Event {
	return newEvent(task, EventInstruction, InstructionSent{RunID: run, Delivery: delivery, Text: text}, at)
}

// NewTranscriptEvent returns a transcript-tier event whose payload is the
// agent's observation as JSON. The domain does not know the agent's types.
func NewTranscriptEvent(task ID, payload []byte, at time.Time) Event {
	return Event{TaskID: task, Kind: EventTranscript, Tier: TierTranscript, Payload: payload, At: at}
}

// TaskHeld is the payload of EventTaskHeld: who wrote the issue and a hash of
// the exact text the human is asked about, so a later start can check that the
// issue has not changed since (the Decision shows only a capped copy).
type TaskHeld struct {
	DecisionID  ID     `json:"decision_id"`
	Author      string `json:"author"`
	Association string `json:"association"`
	TextSHA256  string `json:"text_sha256"`
}

// TextHash is the hash recorded for held text.
func TextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
