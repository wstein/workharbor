package domain

import (
	"slices"
	"time"
)

// Rules a Decision's state can break, reported as conflicts (exit code 5).
const (
	RuleDecisionClosed  Rule = "decision-closed"
	RuleDecisionExpired Rule = "decision-expired"
	RuleSHAMismatch     Rule = "sha-mismatch"
	RuleNotRunBound     Rule = "not-run-bound"
	RuleNotSuperseded   Rule = "not-superseded"
	RuleAlreadyRaised   Rule = "already-raised"
)

// Errors returned when a Decision is resolved. The first group are
// conflicts: they exit with Conflict and match with errors.Is. The second
// group is bad input.
var (
	ErrDecisionClosed  = conflict(RuleDecisionClosed, "decision is not open")
	ErrDecisionExpired = conflict(RuleDecisionExpired, "decision expired before it was answered")
	ErrSHAMismatch     = conflict(RuleSHAMismatch, "allow was given for a different commit, so it is a denial")
	ErrNotRunBound     = conflict(RuleNotRunBound, "only a decision raised by a run is superseded by a restart")
	ErrNotSuperseded   = conflict(RuleNotSuperseded, "only a superseded decision can be raised again")
	ErrAlreadyRaised   = conflict(RuleAlreadyRaised, "decision was already raised again")

	ErrDecisionOption = invalid("answer is not one of the options")
	ErrDecisionActor  = invalid("an answer needs the actor who gave it")
)

// Response is a human's answer to a Decision.
type Response struct {
	By     string    // the actor who answered
	Option string    // one of the Decision's options
	Reason string    // optional, passed back to the agent
	SHA    string    // the commit the human was shown; checked when the Decision has one
	At     time.Time // when the answer arrived
}

// Respond records an answer. It fails closed:
//
//   - an answer without the time it arrived is refused with ErrDecisionTime;
//   - an answer that arrives at or after the deadline is refused with
//     ErrDecisionExpired and the Decision expires, so a late allow never counts;
//   - an allow for a different commit than the Decision's SHA is recorded as a
//     denial and reported as ErrSHAMismatch;
//   - a Decision that is no longer open cannot be answered.
func (d *Decision) respond(r Response) error {
	if d.Status != DecisionOpen {
		return ErrDecisionClosed
	}
	if r.By == "" {
		return ErrDecisionActor
	}
	if r.At.IsZero() {
		return ErrDecisionTime
	}
	if d.pastDeadline(r.At) {
		if err := d.move(DecisionExpired, r.At); err != nil {
			return err
		}
		return ErrDecisionExpired
	}
	if len(d.Options) > 0 && !slices.Contains(d.Options, r.Option) {
		return ErrDecisionOption
	}

	if err := d.move(DecisionAnswered, r.At); err != nil {
		return err
	}
	at := r.At
	d.AnsweredAt = &at
	d.AnsweredBy = r.By
	d.Reason = r.Reason
	d.Answer = r.Option
	var mismatch bool
	if d.SHA != "" && r.Option == AnswerAllow && r.SHA != d.SHA {
		d.Answer = AnswerDeny
		mismatch = true
	}
	d.record(EventDecisionAnswered, AnswerRecorded{ID: d.ID, Answer: d.Answer, By: r.By, Reason: d.Reason, SHA: r.SHA, At: r.At}, r.At)
	if mismatch {
		return ErrSHAMismatch
	}
	return nil
}

// pastDeadline reports whether the Decision's time is up at a moment. An
// approval without a deadline is always past it: every approval has one, so a
// missing one is a lost value, and a lost deadline must not let a late allow
// through. A question or a review Decision without a deadline waits.
func (d *Decision) pastDeadline(at time.Time) bool {
	if d.Deadline.IsZero() {
		return d.Kind == DecisionApproval
	}
	return !at.Before(d.Deadline)
}

// Expire expires an open Decision whose deadline has passed and reports
// whether it changed anything. An expired Decision denies.
func (d *Decision) expire(now time.Time) bool {
	if d.Status != DecisionOpen || !d.pastDeadline(now) {
		return false
	}
	return d.move(DecisionExpired, now) == nil
}

// Supersede marks an open Decision superseded because the supervisor
// restarted: the agent process that was waiting for it is gone. Only a
// Decision raised by a run is superseded; a review Decision survives.
func (d *Decision) supersede() error {
	if d.Status != DecisionOpen {
		return ErrDecisionClosed
	}
	if d.RunID == "" {
		return ErrNotRunBound
	}
	return d.move(DecisionSuperseded, time.Time{})
}

// Reraise opens a new Decision with the same ask for the resumed run, with a
// new ID and a fresh deadline of the same length, and links the superseded one
// to it. Reconciliation (design §5.3) calls it when a run resumes.
func (d *Decision) reraise(id ID, now time.Time) (*Decision, error) {
	if d.Status != DecisionSuperseded {
		return nil, ErrNotSuperseded
	}
	if d.SupersededBy != "" {
		return nil, ErrAlreadyRaised
	}
	n, err := raise(NewDecision{
		ID:       id,
		TaskID:   d.TaskID,
		RunID:    d.RunID,
		Kind:     d.Kind,
		Blocking: d.Blocking,
		Subject:  d.Subject,
		Input:    d.Input,
		SHA:      d.SHA,
		Options:  d.Options,
		Cause:    d.Cause,
		Host:     d.Host,
		Now:      now,
		Timeout:  d.Timeout,
	})
	if err != nil {
		return nil, err
	}
	n.InputTruncated = d.InputTruncated
	d.SupersededBy = id
	d.record(EventDecisionReraised, Reraised{ID: d.ID, NewID: id}, now)
	return n, nil
}

// Allows is the gate an action must pass: it is true only for an answered
// approval or review whose answer is allow and, if the Decision is tied to a
// commit, only for that same commit. An open, expired or superseded Decision,
// a denial and a question never allow, and an allow for an earlier revision
// does not cover a later one.
func (d *Decision) Allows(sha string) bool {
	if d.Kind == DecisionQuestion || d.Status != DecisionAnswered || d.Answer != AnswerAllow {
		return false
	}
	if d.Kind == DecisionApproval && d.Deadline.IsZero() {
		return false // a lost deadline: fail closed
	}
	return d.SHA == "" || d.SHA == sha
}
