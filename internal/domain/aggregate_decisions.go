package domain

import (
	"fmt"
	"strings"
	"time"
)

func (a *TaskAggregate) decision(id ID) (*Decision, error) {
	for _, d := range a.decisions {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, &NotFoundError{Kind: "decision", ID: string(id)}
}

// absorb moves the events a Decision recorded into the aggregate's own, so the
// store writes one stream in the order things happened.
func (a *TaskAggregate) absorb(d *Decision) {
	a.events = append(a.events, d.TakeEvents()...)
}

// supersedeOpen supersedes the open questions and approvals a run raised. The
// login, quota and failed-run questions stay open: they were raised for a run
// that is already paused or over, and nothing waits on them (D23). Review
// Decisions belong to no run.
func (a *TaskAggregate) supersedeOpen(runID ID) {
	for _, d := range a.decisions {
		if d.RunID == runID && d.Status == DecisionOpen && d.Cause == "" {
			if d.supersede() == nil {
				a.absorb(d)
			}
		}
	}
}

// settle moves an awaiting_guidance task back to running once no blocking
// Decision of a run is open.
func (a *TaskAggregate) settle() {
	if a.task.State != TaskAwaitingGuidance {
		return
	}
	for _, d := range a.decisions {
		if d.Status == DecisionOpen && d.RaisesGuidance() {
			return
		}
	}
	_ = a.moveTask(TaskRunning) // awaiting_guidance to running is always legal
}

// RaiseDecision opens a Decision for the task. A question or approval must come
// from a live run of the task; a review Decision needs a task in
// ready_for_review and the pinned commit it is about. Raising a blocking
// Decision from a run moves a running task to awaiting_guidance (D13). A
// refused raise changes nothing.
func (a *TaskAggregate) RaiseDecision(spec NewDecision) (Decision, error) {
	if spec.TaskID != "" && spec.TaskID != a.task.ID {
		return Decision{}, conflict(RuleDecisionTask, "decision %s is for task %s, not %s", spec.ID, spec.TaskID, a.task.ID)
	}
	spec.TaskID = a.task.ID
	if _, err := a.decision(spec.ID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", spec.ID, a.task.ID)
	}
	d, err := raise(spec)
	if err != nil {
		return Decision{}, err
	}
	if d.RunID != "" {
		run, err := a.run(d.RunID)
		if err != nil {
			return Decision{}, err
		}
		if run.State != RunStarting && run.State != RunRunning {
			return Decision{}, conflict(RuleRunLive, "run %s is %s and raises no decision", run.ID, run.State)
		}
	} else if err := a.checkReview(d); err != nil {
		return Decision{}, err
	}
	a.addDecision(d)
	return *d, nil
}

// checkReview checks a review Decision against the task and its candidates.
func (a *TaskAggregate) checkReview(d *Decision) error {
	if a.task.State != TaskReadyForReview {
		return conflict(RuleTaskState, "task %s is %s: a review decision needs ready_for_review", a.task.ID, a.task.State)
	}
	for _, c := range a.candidates {
		if c.SHA == d.SHA {
			return nil
		}
	}
	return &NotFoundError{Kind: "commit", ID: d.SHA}
}

// addDecision adds a checked Decision and applies its effect on the task.
func (a *TaskAggregate) addDecision(d *Decision) {
	a.decisions = append(a.decisions, d)
	a.absorb(d)
	if d.RaisesGuidance() && a.task.State == TaskRunning {
		_ = a.moveTask(TaskAwaitingGuidance) // running to awaiting_guidance is legal
	}
}

// Answer records a human's answer to a Decision and applies what follows from
// it (design §4.1, §4.2). An answer a Decision refuses can still change it,
// since a late answer expires it, and that change is kept with the error; the
// task is settled either way. The login and quota questions act on the run:
// "resume" resumes it (checked before the answer is recorded, so a refusal
// changes nothing), "resume_at_reset" leaves it paused until DueResumes says
// it is time, and "cancel" cancels the task. "retry" of a failed run frees the
// task for a new run.
func (a *TaskAggregate) Answer(id ID, r Response) error {
	d, err := a.decision(id)
	if err != nil {
		return err
	}
	if d.Cause != "" && d.Status == DecisionOpen && r.Option == AnswerResume && d.Cause != CauseRunFailed {
		if _, err := a.checkResume(d.RunID); err != nil {
			return err
		}
	}
	respErr := d.respond(r)
	a.absorb(d)
	if respErr != nil {
		a.settle()
		return respErr
	}
	switch {
	case d.Cause != "" && d.Answer == AnswerCancel:
		return a.Cancel()
	case d.Cause != "" && d.Cause != CauseRunFailed && d.Answer == AnswerResume:
		run, err := a.run(d.RunID)
		if err != nil {
			return err
		}
		return a.resume(run)
	}
	a.settle()
	return nil
}

// DueResumes returns the paused runs whose "resume at reset" answer has come
// due: the reset time the agent reported has passed. The reconciler resumes
// them with Resume.
func (a *TaskAggregate) DueResumes(now time.Time) []ID {
	var due []ID
	for _, run := range a.runs {
		if run.State != RunPaused {
			continue
		}
		var last *Decision
		for _, d := range a.decisions {
			if d.RunID == run.ID && d.Cause == CauseQuotaExhausted && d.Status == DecisionAnswered {
				last = d
			}
		}
		if last != nil && last.Answer == AnswerResumeAtReset && !last.ResumeAt.IsZero() && !now.Before(last.ResumeAt) {
			due = append(due, run.ID)
		}
	}
	return due
}

// SuspendRun pauses a running run because its login expired or its quota is
// used up, supersedes the open questions and approvals it raised, and raises
// the blocking question that asks the human what to do (design §4.2, D23). The
// question has no deadline and fixed options; "resume at reset" is offered only
// when the reset time is known. The task moves to awaiting_guidance.
func (a *TaskAggregate) SuspendRun(runID ID, cause DecisionCause, resetAt time.Time, id ID, now time.Time) (Decision, error) {
	var subject string
	options := []string{AnswerResume, AnswerCancel}
	switch cause {
	case CauseAuthExpired:
		subject = "The login expired"
	case CauseQuotaExhausted:
		subject = "The usage limit is reached"
		if !resetAt.IsZero() {
			options = []string{AnswerResume, AnswerResumeAtReset, AnswerCancel}
		}
	default:
		return Decision{}, invalid(fmt.Sprintf("cause %q does not suspend a run", cause))
	}
	run, err := a.run(runID)
	if err != nil {
		return Decision{}, err
	}
	if run.State != RunRunning {
		return Decision{}, conflict(RuleTransition, "run %s: illegal transition %s -> %s", run.ID, run.State, RunPaused)
	}
	if _, err := a.decision(id); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", id, a.task.ID)
	}
	d, err := raise(NewDecision{
		ID: id, TaskID: a.task.ID, RunID: runID, Kind: DecisionQuestion, Blocking: true, Subject: subject,
		Options: options, Cause: cause, ResumeAt: resetAt, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	if err := a.moveRun(run, RunPaused); err != nil {
		return Decision{}, err
	}
	a.supersedeOpen(runID)
	a.addDecision(d)
	return *d, nil
}

// FailRun ends a run as failed and opens the blocking Decision that asks the
// human to retry or cancel (design §4.1): a failed run does not fail its task.
func (a *TaskAggregate) FailRun(runID, decisionID ID, now time.Time) (Decision, error) {
	run, err := a.run(runID)
	if err != nil {
		return Decision{}, err
	}
	if !run.State.CanTransition(RunFailed) {
		return Decision{}, run.transition(RunFailed)
	}
	if _, err := a.decision(decisionID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", decisionID, a.task.ID)
	}
	d, err := raise(NewDecision{
		ID: decisionID, TaskID: a.task.ID, RunID: runID, Kind: DecisionQuestion, Blocking: true, Subject: "The run failed",
		Options: []string{AnswerRetry, AnswerCancel}, Cause: CauseRunFailed, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	if err := a.moveRun(run, RunFailed); err != nil {
		return Decision{}, err
	}
	a.supersedeOpen(runID)
	a.addDecision(d)
	return *d, nil
}

// RaiseRebaseConflict opens the blocking question for a stopped run whose
// branch does not rebase onto the integration branch before the export (design
// §4.2), as the login and quota questions are raised for a paused run: the
// task moves to awaiting_guidance with no change to the state machines. The
// options are fixed: rework (a new run on the agent), retry after the human
// fixed it in the console, and cancel. The conflicting paths go in the input as
// untrusted data, capped like any agent input.
func (a *TaskAggregate) RaiseRebaseConflict(runID, decisionID ID, target string, paths []string, now time.Time) (Decision, error) {
	run, err := a.run(runID)
	if err != nil {
		return Decision{}, err
	}
	if run.State != RunStopped {
		return Decision{}, conflict(RuleRunLive, "run %s is %s: a rebase conflict is raised for a stopped run", run.ID, run.State)
	}
	if _, err := a.decision(decisionID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", decisionID, a.task.ID)
	}
	d, err := raise(NewDecision{
		ID: decisionID, TaskID: a.task.ID, RunID: runID, Kind: DecisionQuestion, Blocking: true,
		Subject: "The agent's branch does not rebase onto " + target, Input: strings.Join(paths, "\n"),
		Options: []string{AnswerRework, AnswerRetry, AnswerCancel}, Cause: CauseRebaseConflict, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	a.addDecision(d)
	return *d, nil
}

// RaiseRunFailedAgain asks the retry-or-cancel question again for a run that
// already failed, when the retry the human chose could not be carried out (the
// new run could not start): without it the task would be left running with no
// run and no question. The run must be failed.
func (a *TaskAggregate) RaiseRunFailedAgain(runID, decisionID ID, now time.Time) (Decision, error) {
	run, err := a.run(runID)
	if err != nil {
		return Decision{}, err
	}
	if run.State != RunFailed {
		return Decision{}, conflict(RuleRunLive, "run %s is %s: only a failed run is asked about again", run.ID, run.State)
	}
	if _, err := a.decision(decisionID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", decisionID, a.task.ID)
	}
	d, err := raise(NewDecision{
		ID: decisionID, TaskID: a.task.ID, RunID: runID, Kind: DecisionQuestion, Blocking: true, Subject: "The run failed",
		Options: []string{AnswerRetry, AnswerCancel}, Cause: CauseRunFailed, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	a.addDecision(d)
	return *d, nil
}

// RaiseUntrustedHold holds a task that has no run yet because its issue is by
// an author who is not trusted (design §6, issue #53): a blocking question asks
// the human to start it or cancel it. The author and the issue text are in the
// input, as untrusted data, capped like any agent input. The task stays queued:
// the state machines do not change, and no run exists until the human says
// start. The task is marked as having untrusted input.
func (a *TaskAggregate) RaiseUntrustedHold(decisionID ID, author, association, text string, now time.Time) (Decision, error) {
	if a.task.State != TaskQueued || len(a.runs) > 0 {
		return Decision{}, conflict(RuleTaskState, "task %s is %s: only a queued task with no run is held", a.task.ID, a.task.State)
	}
	if _, err := a.decision(decisionID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", decisionID, a.task.ID)
	}
	d, err := raise(NewDecision{
		ID: decisionID, TaskID: a.task.ID, Kind: DecisionQuestion, Blocking: true,
		Subject: "Start a run on an issue by an untrusted author?",
		Input:   "author: " + author + " (" + association + ")\n" + text,
		Options: []string{AnswerStart, AnswerCancel}, Cause: CauseUntrustedInput, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	a.task.Untrusted = true
	a.addDecision(d)
	a.record(EventTaskHeld, TaskHeld{DecisionID: decisionID, Author: author, Association: association, TextSHA256: TextHash(text)})
	return *d, nil
}

// MarkRunning moves a starting run to running once the agent is up.
func (a *TaskAggregate) MarkRunning(runID ID) error {
	run, err := a.run(runID)
	if err != nil {
		return err
	}
	if err := a.moveRun(run, RunRunning); err != nil {
		return err
	}
	run.ResumeAttempts = 0
	return nil
}

// RecordSession stores the agent's session ID on the run when the agent reports
// it. The same ID again changes nothing; another ID, or an empty one, is a
// conflict, because a run keeps its session across resumes.
func (a *TaskAggregate) RecordSession(runID ID, sessionID string) error {
	run, err := a.run(runID)
	if err != nil {
		return err
	}
	switch {
	case sessionID == "":
		return conflict(RuleSessionID, "run %s: a session ID cannot be empty", runID)
	case run.SessionID == sessionID:
		return nil
	case run.SessionID != "":
		return conflict(RuleSessionID, "run %s already has session %s, not %s", runID, run.SessionID, sessionID)
	}
	run.SessionID = sessionID
	a.record(EventRunSession, RunSession{RunID: runID, SessionID: sessionID})
	return nil
}

// Cancel stops the task's live run, supersedes every open Decision (a review
// Decision too) and cancels the task. A task that is already over cannot be
// cancelled.
func (a *TaskAggregate) Cancel() error {
	if !a.task.State.CanTransition(TaskCancelled) {
		return a.task.transition(TaskCancelled) // reports the illegal transition
	}
	for _, run := range a.runs {
		if !run.State.Terminal() {
			if err := a.moveRun(run, RunStopped); err != nil {
				return err
			}
		}
	}
	for _, d := range a.decisions {
		if d.Status == DecisionOpen && d.move(DecisionSuperseded, time.Time{}) == nil {
			a.absorb(d)
		}
	}
	return a.moveTask(TaskCancelled)
}

// Interrupt marks a starting, running or paused run interrupted because its
// process or environment is gone (design §4.1, §5.3). It supersedes the open
// questions and approvals the run raised, which are raised again when the agent
// asks again after a resume; the login and quota questions and the review
// Decisions stay.
func (a *TaskAggregate) Interrupt(runID ID) error {
	run, err := a.run(runID)
	if err != nil {
		return err
	}
	if err := a.moveRun(run, RunInterrupted); err != nil {
		return err
	}
	a.supersedeOpen(run.ID)
	a.settle()
	return nil
}

// ObserveEnv records what the runtime reports about an environment. It is a
// fact, so it does not go through the guard that stops a deliberate
// StopEnvironment under a live run; instead every live run in an environment
// seen stopped or gone is interrupted first. An observation that changes
// nothing records nothing.
func (a *TaskAggregate) ObserveEnv(envID ID, state EnvState) error {
	env, err := a.env(envID)
	if err != nil {
		return err
	}
	if env.State == state {
		return nil
	}
	switch state {
	case EnvRunning, EnvStopped, EnvDeleted:
	default:
		return conflict(RuleTransition, "environment %s: illegal transition %s -> %s", env.ID, env.State, state)
	}
	if state == EnvRunning && !env.State.CanTransition(EnvRunning) {
		return conflict(RuleTransition, "environment %s: illegal transition %s -> %s", env.ID, env.State, state)
	}
	if state != EnvRunning {
		for _, run := range a.runs {
			if run.EnvID == envID && !run.State.Terminal() && run.State != RunInterrupted {
				if err := a.Interrupt(run.ID); err != nil {
					return err
				}
			}
		}
		if env.State == EnvRunning {
			if err := a.moveEnv(env, EnvStopped); err != nil {
				return err
			}
		}
		if state == EnvStopped || env.State == state {
			return nil
		}
	}
	return a.moveEnv(env, state)
}

// ExpireDecisions expires the open Decisions whose deadline has passed at now
// and returns their IDs. An expired approval denies (design §4.2). A task that
// waited only for them is freed.
func (a *TaskAggregate) ExpireDecisions(now time.Time) []ID {
	var expired []ID
	for _, d := range a.decisions {
		if d.expire(now) {
			a.absorb(d)
			expired = append(expired, d.ID)
		}
	}
	a.settle()
	return expired
}

// ReraiseDecision opens a new Decision with the same ask for a resumed run,
// with a new ID and a fresh deadline of the same length, and links the
// superseded one to it (design §5.3). The run must be live again.
func (a *TaskAggregate) ReraiseDecision(id, newID ID, now time.Time) (Decision, error) {
	old, err := a.decision(id)
	if err != nil {
		return Decision{}, err
	}
	if _, err := a.decision(newID); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists in task %s", newID, a.task.ID)
	}
	run, err := a.run(old.RunID)
	if err != nil {
		return Decision{}, err
	}
	if run.State != RunStarting && run.State != RunRunning {
		return Decision{}, conflict(RuleRunLive, "run %s is %s and raises no decision", run.ID, run.State)
	}
	n, err := old.reraise(newID, now)
	if err != nil {
		return Decision{}, err
	}
	a.absorb(old)
	a.addDecision(n)
	return *n, nil
}

// StopRun ends a live run normally: the agent finished, or the run was
// cancelled or stopped by hand. It supersedes what the run asked and frees a
// task that waited for guidance only on it. A failed end is FailRun.
func (a *TaskAggregate) StopRun(runID ID) error {
	run, err := a.run(runID)
	if err != nil {
		return err
	}
	if err := a.moveRun(run, RunStopped); err != nil {
		return err
	}
	a.supersedeOpen(run.ID)
	a.settle()
	return nil
}

// ResumeBlocked says why a run cannot be resumed no matter what its
// environment does: its state does not allow it, or a login or quota question
// of the run is still open. The reconciler asks before it starts a container,
// so a run that waits for the human costs none. nil means Resume's own
// conditions hold except the environment.
func (a *TaskAggregate) ResumeBlocked(runID ID) error {
	run, err := a.run(runID)
	if err != nil {
		return err
	}
	if !run.State.CanTransition(RunStarting) {
		return run.transition(RunStarting) // reports the illegal transition
	}
	for _, d := range a.decisions {
		if d.RunID == run.ID && d.Status == DecisionOpen && d.Cause != "" {
			return conflict(RuleDecisionOpen, "run %s cannot resume: decision %s (%s) is still open", run.ID, d.ID, d.Cause)
		}
	}
	return nil
}

// WaitsForReset reports whether the run's latest quota question was answered
// "resume at reset" and the reset time has not come, so the run is to stay
// down, interrupted or paused, until then.
func (a *TaskAggregate) WaitsForReset(runID ID, now time.Time) bool {
	var last *Decision
	for _, d := range a.decisions {
		if d.RunID == runID && d.Cause == CauseQuotaExhausted && d.Status == DecisionAnswered {
			last = d
		}
	}
	return last != nil && last.Answer == AnswerResumeAtReset && !last.ResumeAt.IsZero() && now.Before(last.ResumeAt)
}

// RecordLaunchFailure counts a failed launch of the agent for a starting run,
// returns it to interrupted so the next pass tries again, and reports whether
// the attempts are used up. Then the caller ends the run with FailRun.
func (a *TaskAggregate) RecordLaunchFailure(runID ID, limit int) (exhausted bool, err error) {
	run, err := a.run(runID)
	if err != nil {
		return false, err
	}
	if run.State != RunStarting {
		return false, conflict(RuleTransition, "run %s: a launch fails only for a starting run, not %s", run.ID, run.State)
	}
	run.ResumeAttempts++
	a.record(EventRunAttempt, RunAttempt{RunID: run.ID, Attempts: run.ResumeAttempts})
	if run.ResumeAttempts >= limit {
		return true, a.Interrupt(run.ID) // FailRun takes over from interrupted
	}
	return false, a.Interrupt(run.ID)
}

// SupersededOf returns the Decisions of a run that were superseded and not
// raised again, for the resume briefing (design D27).
func (a *TaskAggregate) SupersededOf(runID ID) []Decision {
	var out []Decision
	for _, d := range a.decisions {
		if d.RunID == runID && d.Status == DecisionSuperseded && d.SupersededBy == "" {
			out = append(out, *d)
		}
	}
	return out
}
