package domain

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// DecisionKind classifies what the human is being asked.
type DecisionKind string

const (
	DecisionQuestion DecisionKind = "question"
	DecisionApproval DecisionKind = "approval"
	DecisionReview   DecisionKind = "review"
)

// DecisionStatus is the lifecycle state of a Decision (design §4.2). Open
// becomes answered, expired or superseded, and each of those is terminal.
type DecisionStatus string

const (
	DecisionOpen       DecisionStatus = "open"
	DecisionAnswered   DecisionStatus = "answered"
	DecisionExpired    DecisionStatus = "expired"
	DecisionSuperseded DecisionStatus = "superseded"
)

var decisionTransitions = map[DecisionStatus][]DecisionStatus{
	DecisionOpen: {DecisionAnswered, DecisionExpired, DecisionSuperseded},
}

// CanTransition reports whether a Decision may move from one status to another.
func (s DecisionStatus) CanTransition(to DecisionStatus) bool {
	return can(decisionTransitions, s, to)
}

// Terminal reports whether no transition leaves the status.
func (s DecisionStatus) Terminal() bool { return s != DecisionOpen }

// The two answers of an approval.
const (
	AnswerAllow = "allow"
	AnswerDeny  = "deny"
)

const (
	// MaxDecisionInput is the most characters of an agent's input a Decision
	// keeps; the full input stays with the agent (design §4.2).
	MaxDecisionInput = 2000

	// DefaultApprovalTimeout is how long an approval waits before it expires
	// and denies. Spike #1 used ten minutes.
	DefaultApprovalTimeout = 10 * time.Minute
)

// Decision is a request raised to the human. A blocking Decision raised by a
// live run puts its task into TaskAwaitingGuidance; the review Decisions of
// TaskReadyForReview ("Ready to push?") leave the task where it is.
//
// Everything in Subject and Input comes from an agent or a repository and is
// untrusted data. A Decision fails closed: only an answered allow permits
// anything (see Allows).
type Decision struct {
	ID       ID
	TaskID   ID
	RunID    ID // the run that raised it; empty for a review Decision
	Kind     DecisionKind
	Blocking bool

	Subject        string // what is asked: a question, a tool name or a plan
	Input          string // capped copy of the agent's input (MaxDecisionInput)
	InputTruncated bool
	SHA            string // the commit this decision is about; set for review Decisions
	Options        []string

	Status     DecisionStatus
	CreatedAt  time.Time
	Timeout    time.Duration // how long it waits; kept so a re-raise uses the same
	Deadline   time.Time     // zero means none; every approval has one
	AnsweredAt *time.Time
	Answer     string
	Reason     string // optional, passed back to the agent
	AnsweredBy string // the actor who answered

	SupersededBy ID // the Decision raised again after a restart
}

// NewDecision describes a Decision to raise.
type NewDecision struct {
	ID       ID
	TaskID   ID
	RunID    ID
	Kind     DecisionKind
	Blocking bool
	Subject  string
	Input    string
	SHA      string
	Options  []string
	Now      time.Time
	Timeout  time.Duration // zero: DefaultApprovalTimeout for an approval, none otherwise
}

// Validation errors returned by Raise.
var (
	ErrDecisionID   = errors.New("decision needs an ID and a task")
	ErrDecisionKind = errors.New("unknown decision kind")
	ErrDecisionRun  = errors.New("a review decision has no run, and any other decision needs one")
	ErrDecisionSHA  = errors.New("a review decision needs the commit SHA it is about")

	ErrDecisionTimeout = errors.New("a decision timeout cannot be negative")
	ErrDecisionTime    = errors.New("a time is needed and it is zero")
)

// Raise creates an open Decision. It caps the input, gives an approval a
// deadline, gives an approval or review the allow and deny options, and checks that a review Decision
// names its commit and that only a review Decision is raised without a run.
func Raise(spec NewDecision) (*Decision, error) {
	if spec.ID == "" || spec.TaskID == "" {
		return nil, ErrDecisionID
	}
	if spec.Timeout < 0 {
		return nil, ErrDecisionTimeout
	}
	if spec.Now.IsZero() {
		return nil, ErrDecisionTime
	}
	switch spec.Kind {
	case DecisionQuestion, DecisionApproval:
		if spec.RunID == "" {
			return nil, ErrDecisionRun
		}
	case DecisionReview:
		if spec.RunID != "" {
			return nil, ErrDecisionRun
		}
		if spec.SHA == "" {
			return nil, ErrDecisionSHA
		}
	default:
		return nil, fmt.Errorf("%w: %q", ErrDecisionKind, spec.Kind)
	}

	d := &Decision{
		ID:        spec.ID,
		TaskID:    spec.TaskID,
		RunID:     spec.RunID,
		Kind:      spec.Kind,
		Blocking:  spec.Blocking,
		Subject:   spec.Subject,
		SHA:       spec.SHA,
		Options:   append([]string(nil), spec.Options...),
		Status:    DecisionOpen,
		CreatedAt: spec.Now,
	}
	d.Input, d.InputTruncated = capInput(spec.Input)
	if d.Kind != DecisionQuestion && len(d.Options) == 0 {
		d.Options = []string{AnswerAllow, AnswerDeny}
	}
	timeout := spec.Timeout
	if timeout == 0 && d.Kind == DecisionApproval {
		timeout = DefaultApprovalTimeout
	}
	if timeout > 0 {
		d.Timeout = timeout
		d.Deadline = spec.Now.Add(timeout)
	}
	return d, nil
}

// capInput keeps at most MaxDecisionInput characters.
func capInput(s string) (string, bool) {
	if utf8.RuneCountInString(s) <= MaxDecisionInput {
		return s, false
	}
	n := 0
	for i := range s {
		if n == MaxDecisionInput {
			return s[:i], true
		}
		n++
	}
	return s, false
}

// RaisesGuidance reports whether the Decision moves its task to
// TaskAwaitingGuidance: only a blocking Decision raised by a live run does
// (D13). A review Decision leaves the task in TaskReadyForReview.
func (d *Decision) RaisesGuidance() bool {
	return d.Blocking && d.RunID != ""
}

// move changes the status through the transition table. Every status change
// goes through it, so none can skip the table.
func (d *Decision) move(to DecisionStatus) error {
	if !d.Status.CanTransition(to) {
		return conflict(RuleTransition, "decision %s: illegal transition %s -> %s", d.ID, d.Status, to)
	}
	d.Status = to
	return nil
}
