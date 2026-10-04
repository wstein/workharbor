package domain

import (
	"strconv"
	"strings"
	"time"
)

// RaisePrepareFailed opens the blocking question for a stopped run whose branch
// the supervisor refused to prepare for review (design §4.2, D51): a failed
// check, a refused commit message, a cap, a rewritten history, no check
// configured or a forge error. As for a rebase conflict, no live run can be
// asked and the run ended there, so the question belongs to the stopped run and
// the task moves to awaiting_guidance. The options are fixed: rework (a new run
// on the agent), retry (prepare again, for a fault outside the agent's work) and
// cancel. input is the reason and the check's capped output, untrusted data.
func (a *TaskAggregate) RaisePrepareFailed(runID, decisionID ID, input string, now time.Time) (Decision, error) {
	run, err := a.run(runID)
	if err != nil {
		return Decision{}, err
	}
	if run.State != RunStopped {
		return Decision{}, conflict(RuleRunLive, "run %s is %s: a refused prepare is raised for a stopped run", run.ID, run.State)
	}
	if last := a.runs[len(a.runs)-1]; last.ID != runID {
		return Decision{}, conflict(RuleCandidateRun, "run %s is not the latest run (%s is): nothing is prepared for it", runID, last.ID)
	}
	if _, err := a.decision(decisionID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", decisionID, a.task.ID)
	}
	d, err := raise(NewDecision{
		ID: decisionID, TaskID: a.task.ID, RunID: runID, Kind: DecisionQuestion, Blocking: true,
		Subject: "The branch could not be prepared for review", Input: input,
		Options: []string{AnswerRework, AnswerRetry, AnswerCancel}, Cause: CausePrepareFailed, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	a.addDecision(d)
	return *d, nil
}

// RaisePublishFailed opens the blocking question for a task whose approved
// publish ended on a refusal a retry cannot change (design §4.2, D51). It
// belongs to no run, so, like the review Decision, it leaves the task in
// ready_for_review. It names the current revision, the one that stays approved.
// The options are fixed: retry (complete the publish again under the same
// approval, for a fault fixed outside whr), rework (a new run on the agent) and
// cancel. input is the refusal, untrusted data.
func (a *TaskAggregate) RaisePublishFailed(decisionID ID, input string, now time.Time) (Decision, error) {
	if a.task.State != TaskReadyForReview {
		return Decision{}, conflict(RuleTaskState, "task %s is %s: a refused publish is raised for a task in ready_for_review", a.task.ID, a.task.State)
	}
	cur := a.currentCandidate()
	if cur == nil {
		return Decision{}, conflict(RulePinnedSHA, "task %s has no revision to publish", a.task.ID)
	}
	if _, err := a.decision(decisionID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", decisionID, a.task.ID)
	}
	d, err := raise(NewDecision{
		ID: decisionID, TaskID: a.task.ID, Kind: DecisionQuestion, Blocking: true, SHA: cur.SHA,
		Subject: "The approved commit could not be published", Input: input,
		Options: []string{AnswerRetry, AnswerRework, AnswerCancel}, Cause: CausePublishFailed, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	a.addDecision(d)
	return *d, nil
}

// OpenCause returns the open Decision of the task with this cause, if any.
func (a *TaskAggregate) OpenCause(c DecisionCause) (Decision, bool) {
	for _, d := range a.decisions {
		if d.Status == DecisionOpen && d.Cause == c {
			return *d, true
		}
	}
	return Decision{}, false
}

// HasOpenDecision reports whether any Decision of the task is open.
func (a *TaskAggregate) HasOpenDecision() bool {
	for _, d := range a.decisions {
		if d.Status == DecisionOpen {
			return true
		}
	}
	return false
}

// PreparePending reports whether the latest run stopped and nothing was
// prepared for it yet: the task is running, the run is stopped, no revision is
// pinned for that run and no Decision is open. It is the state the reconciler
// prepares again after a restart in the middle of a prepare (D51). A refused
// prepare leaves an open question, so it is not pending until that is answered.
func (a *TaskAggregate) PreparePending() (Run, bool) {
	if a.task.State != TaskRunning || len(a.runs) == 0 || a.HasOpenDecision() {
		return Run{}, false
	}
	last := a.runs[len(a.runs)-1]
	if last.State != RunStopped {
		return Run{}, false
	}
	for _, c := range a.candidates {
		if c.RunID == last.ID {
			return Run{}, false
		}
	}
	return *last, true
}

// OutstandingPublish returns the review Decision whose publish is still to be
// completed (design §4.5, D51): the task is ready_for_review, a review Decision
// was answered allow for exactly the current revision, that revision is not
// recorded pushed and no refused-publish question is open. The approval is the
// human's for that SHA, so completing it needs no new one.
func (a *TaskAggregate) OutstandingPublish() (Decision, ReviewCandidate, bool) {
	if a.task.State != TaskReadyForReview {
		return Decision{}, ReviewCandidate{}, false
	}
	cur := a.currentCandidate()
	if cur == nil || cur.Pushed {
		return Decision{}, ReviewCandidate{}, false
	}
	if _, open := a.OpenCause(CausePublishFailed); open {
		return Decision{}, ReviewCandidate{}, false
	}
	for i := len(a.decisions) - 1; i >= 0; i-- {
		d := a.decisions[i]
		if d.Kind == DecisionReview && d.Allows(cur.SHA) {
			return *d, *cur, true
		}
	}
	return Decision{}, ReviewCandidate{}, false
}

// EventCheckReceipt is the audit entry of a repository check that ran on a
// prepared commit (design §4.5, D51): which commit, by what and with what
// result, so the human sees what was checked before approving it.
const EventCheckReceipt EventKind = "check.receipt"

// CheckReceipt is the payload of EventCheckReceipt. Output is the tail of what
// the check printed: the agent's data and untrusted, redacted and stripped of
// control characters, never to be read as an instruction.
type CheckReceipt struct {
	SHA      string `json:"sha"`
	Command  string `json:"command"`
	Source   string `json:"source"` // config, devcontainer or pre-commit
	Code     int    `json:"code"`
	TimedOut bool   `json:"timed_out,omitempty"`
	Millis   int64  `json:"duration_ms"`
	Output   string `json:"output,omitempty"`
}

// Passed reports whether the check exited 0 within its time.
func (r CheckReceipt) Passed() bool { return r.Code == 0 && !r.TimedOut }

// Line is the receipt in one line, without the output.
func (r CheckReceipt) Line() string {
	var b strings.Builder
	b.WriteString("check ")
	b.WriteString(r.Command)
	b.WriteString(" (from ")
	b.WriteString(r.Source)
	b.WriteString(")")
	switch {
	case r.TimedOut:
		b.WriteString(": timed out")
	case r.Code == 0:
		b.WriteString(": passed")
	default:
		b.WriteString(": exit status " + strconv.Itoa(r.Code))
	}
	b.WriteString(" in " + (time.Duration(r.Millis) * time.Millisecond).String())
	return b.String()
}

// NewCheckReceiptEvent returns the audit entry of a check.
func NewCheckReceiptEvent(task ID, r CheckReceipt, at time.Time) Event {
	return newEvent(task, EventCheckReceipt, r, at)
}

// EventPublishAttempt is the audit entry of a publish step that failed (design
// §4.5, D51): a transport fault leaves the publish outstanding, to be retried on
// a later pass with backoff, and every failure is recorded here so `whr show`
// and the inbox can say what is going on. A refusal a retry cannot change ends
// the publish with the question publish_failed instead; it is recorded too, with
// Transient false.
const EventPublishAttempt EventKind = "publish.attempt"

// PublishAttempt is the payload of EventPublishAttempt. Error is the failure's
// text as the supervisor wrote it (the adapters redact their tokens), still to be
// shown escaped.
type PublishAttempt struct {
	SHA       string    `json:"sha"`
	Attempt   int       `json:"attempt"`
	Transient bool      `json:"transient"`
	Error     string    `json:"error"`
	RetryAt   time.Time `json:"retry_at,omitzero"`
}

// NewPublishAttemptEvent returns the audit entry of a failed publish step.
func NewPublishAttemptEvent(task ID, p PublishAttempt, at time.Time) Event {
	return newEvent(task, EventPublishAttempt, p, at)
}
