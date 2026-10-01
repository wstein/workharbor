package domain

import (
	"fmt"
	"time"
)

// Rules of design §4.1 that couple the task, run and environment machines.
const (
	RuleRunAgent     Rule = "run-agent"     // a run uses the agent its task is assigned to (design D42)
	RuleOneLiveRun   Rule = "one-live-run"  // a task has at most one run that is not stopped or failed
	RuleRunReused    Rule = "run-reused"    // StartRun takes a run that has not started
	RuleRunID        Rule = "run-id"        // a run has a non-empty ID that is unique in the task
	RuleCandidateRun Rule = "candidate-run" // the current revision was produced by the latest run
	RuleTaskState    Rule = "task-state"    // a run starts only on a queued, running or ready_for_review task
	RuleEnvRunning   Rule = "env-running"   // a run is created or started only in a running environment
	RuleEnvInUse     Rule = "env-in-use"    // an environment is not stopped under a starting or running run
	RuleStoppedRun   Rule = "stopped-run"   // ready_for_review needs the task's latest run to be stopped
	RulePinnedSHA    Rule = "pinned-sha"    // ready_for_review needs a pinned commit
	RuleCIPassed     Rule = "ci-passed"     // where CI is required, the current commit must have passed

	RuleDecisionID   Rule = "decision-id"   // a Decision has an ID that is unique in the task
	RuleDecisionTask Rule = "decision-task" // a Decision belongs to the aggregate's task
	RuleRunLive      Rule = "run-live"      // a run raises Decisions only while it is live
	RuleDecisionOpen Rule = "decision-open" // a run does not resume under an open login or quota question
	RuleSessionID    Rule = "session-id"    // a run keeps the session ID it reported
)

// TaskAggregate is a task with the runs, environments and review candidates
// it is coupled to. The task, run and environment machines are each valid on
// their own; the methods here are the guards that keep them consistent with
// one another (design §4.1). A violation is a *ConflictError, which maps to
// exit code Conflict; an unknown run or environment is a *NotFoundError.
//
// Only the aggregate changes state. Its fields and the machines' Transition
// methods are unexported, so no caller can skip a guard; code outside the
// domain reads through the accessors and a Snapshot, and the store rebuilds an
// aggregate with Restore (design §4.1).
type TaskAggregate struct {
	task Task
	runs []*Run // oldest first
	envs map[ID]*Environment

	// candidates are the prepared revisions, oldest first. The last one is the
	// current revision.
	candidates []*ReviewCandidate

	// decisions are every Decision raised for the task, oldest first.
	decisions []*Decision

	events []Event // recorded changes, taken by TakeEvents
}

// NewTaskAggregate returns an aggregate for task with nothing attached yet.
func NewTaskAggregate(task Task) *TaskAggregate {
	return &TaskAggregate{task: task, envs: map[ID]*Environment{}}
}

// AddEnvironment registers an environment the task's runs may use. It is
// recorded, like every change of state.
func (a *TaskAggregate) AddEnvironment(e Environment) {
	a.envs[e.ID] = &e
	a.record(EventEnvAdded, EnvAdded{ID: e.ID, Backend: e.Backend, State: string(e.State)})
}

// PendingEvents returns the recorded events without forgetting them, so a
// store can write them and forget them only once the write has committed.
func (a *TaskAggregate) PendingEvents() []Event { return append([]Event(nil), a.events...) }

// TakeEvents returns the events the changes since the last call produced and
// forgets them. The store writes the new state and these events in one
// transaction (design §5.4).
func (a *TaskAggregate) TakeEvents() []Event {
	ev := a.events
	a.events = nil
	return ev
}

func (a *TaskAggregate) record(kind EventKind, payload any) {
	a.events = append(a.events, newEvent(a.task.ID, kind, payload, time.Time{}))
}

func (a *TaskAggregate) moveRun(run *Run, to RunState) error {
	from := run.State
	if err := run.transition(to); err != nil {
		return err
	}
	a.record(EventRunState, StateChanged{Object: "run", ID: run.ID, From: string(from), To: string(to)})
	return nil
}

func (a *TaskAggregate) moveEnv(env *Environment, to EnvState) error {
	from := env.State
	if err := env.transition(to); err != nil {
		return err
	}
	a.record(EventEnvState, StateChanged{Object: "environment", ID: env.ID, From: string(from), To: string(to)})
	return nil
}

func (a *TaskAggregate) moveTask(to TaskState) error {
	from := a.task.State
	if err := a.task.transition(to); err != nil {
		return err
	}
	a.record(EventTaskState, StateChanged{Object: "task", ID: a.task.ID, From: string(from), To: string(to)})
	return nil
}

func (a *TaskAggregate) run(id ID) (*Run, error) {
	for _, r := range a.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, &NotFoundError{Kind: "run", ID: string(id)}
}

func (a *TaskAggregate) env(id ID) (*Environment, error) {
	if e, ok := a.envs[id]; ok {
		return e, nil
	}
	return nil, &NotFoundError{Kind: "environment", ID: string(id)}
}

func (a *TaskAggregate) liveRun() *Run {
	for _, r := range a.runs {
		if !r.State.Terminal() {
			return r
		}
	}
	return nil
}

// LiveRun returns a copy of the run that is not yet stopped or failed.
func (a *TaskAggregate) LiveRun() (Run, bool) {
	if r := a.liveRun(); r != nil {
		return *r, true
	}
	return Run{}, false
}

// StartRun adds a new run in an environment and starts it. The run must be new
// (no state yet) with an ID no run of the task has. A task has at most one live
// run, and the environment must be running. Once every guard has passed, a
// queued or ready_for_review (rework) task moves to running in the same change.
func (a *TaskAggregate) StartRun(r Run) error {
	run := &r
	if run.State != "" {
		return conflict(RuleRunReused, "run %s is already %s: a new run is started, a finished one is never reused", run.ID, run.State)
	}
	if run.ID == "" {
		return conflict(RuleRunID, "a run needs an ID")
	}
	for _, r := range a.runs {
		if r.ID == run.ID {
			return conflict(RuleRunID, "run %s already exists in task %s", run.ID, a.task.ID)
		}
	}
	if a.task.AgentID != run.AgentID {
		return conflict(RuleRunAgent, "task %s is assigned to agent %q, so run %s cannot use agent %q", a.task.ID, a.task.AgentID, run.ID, run.AgentID)
	}
	switch a.task.State {
	case TaskQueued, TaskRunning, TaskReadyForReview: // the last is rework
	default:
		return conflict(RuleTaskState, "task %s is %s and starts no run", a.task.ID, a.task.State)
	}
	if live := a.liveRun(); live != nil {
		return conflict(RuleOneLiveRun, "task %s already has a live run %s (%s)", a.task.ID, live.ID, live.State)
	}
	env, err := a.env(run.EnvID)
	if err != nil {
		return err
	}
	if env.State != EnvRunning {
		return conflict(RuleEnvRunning, "run %s needs a running environment, but %s is %s", run.ID, env.ID, env.State)
	}
	if a.task.State != TaskRunning {
		if err := a.moveTask(TaskRunning); err != nil {
			return err
		}
	}
	run.TaskID = a.task.ID
	run.State = RunStarting
	a.runs = append(a.runs, run)
	a.record(EventRunStarted, RunStarted{RunID: run.ID, EnvID: run.EnvID})
	return nil
}

// Pause pauses a running run. It changes the run only: the environment stays
// as it is, because the human may want to inspect or edit in it (design §4.3).
func (a *TaskAggregate) Pause(runID ID) error {
	run, err := a.run(runID)
	if err != nil {
		return err
	}
	env, err := a.env(run.EnvID)
	if err != nil {
		return err
	}
	if env.State != EnvRunning {
		return conflict(RuleEnvRunning, "run %s cannot be paused: its environment %s is %s", run.ID, env.ID, env.State)
	}
	if err := a.moveRun(run, RunPaused); err != nil {
		return err
	}
	// The agent process that asked is gone, so nothing could receive an answer (D23).
	a.supersedeOpen(run.ID)
	a.settle()
	return nil
}

// Resume starts a paused or interrupted run again, which relaunches the agent
// from its session. The environment must be running, and no login or quota
// question of the run may be open: answer it, or cancel. Resuming supersedes
// the run's other open Decisions and frees a task that waited for guidance.
func (a *TaskAggregate) Resume(runID ID) error {
	run, err := a.checkResume(runID)
	if err != nil {
		return err
	}
	for _, d := range a.decisions {
		if d.RunID == run.ID && d.Status == DecisionOpen && d.Cause != "" {
			return conflict(RuleDecisionOpen, "run %s cannot resume: decision %s (%s) is still open", run.ID, d.ID, d.Cause)
		}
	}
	return a.resume(run)
}

// checkResume checks what a resume needs of the run and its environment.
func (a *TaskAggregate) checkResume(runID ID) (*Run, error) {
	run, err := a.run(runID)
	if err != nil {
		return nil, err
	}
	if !run.State.CanTransition(RunStarting) {
		return nil, run.transition(RunStarting) // reports the illegal transition
	}
	env, err := a.env(run.EnvID)
	if err != nil {
		return nil, err
	}
	if env.State != EnvRunning {
		return nil, conflict(RuleEnvRunning, "run %s cannot resume: its environment %s is %s", run.ID, env.ID, env.State)
	}
	return run, nil
}

func (a *TaskAggregate) resume(run *Run) error {
	if err := a.moveRun(run, RunStarting); err != nil {
		return err
	}
	a.supersedeOpen(run.ID)
	a.settle()
	return nil
}

// StopEnvironment stops an environment. It is refused while a run in it is
// starting or running: the run is stopped or interrupted first.
func (a *TaskAggregate) StopEnvironment(envID ID) error {
	env, err := a.env(envID)
	if err != nil {
		return err
	}
	for _, r := range a.runs {
		if r.EnvID == envID && (r.State == RunStarting || r.State == RunRunning) {
			return conflict(RuleEnvInUse, "environment %s cannot be stopped: run %s is %s", envID, r.ID, r.State)
		}
	}
	return a.moveEnv(env, EnvStopped)
}

// PinRevision records a prepared revision: the commit SHA that cleanup pinned
// on a branch, before it is pushed (design §4.5), and the run that produced
// it. It becomes the current revision. A SHA is pinned once, and only the
// task's latest run may pin one.
func (a *TaskAggregate) PinRevision(runID ID, branch, sha string) (ReviewCandidate, error) {
	return a.PinPrepared(runID, branch, sha, "")
}

// PinPrepared pins a prepared revision like PinRevision and records the
// agent's own tip it was prepared from (source), which a follow-up round
// rebases from.
func (a *TaskAggregate) PinPrepared(runID ID, branch, sha, source string) (ReviewCandidate, error) {
	if _, err := a.run(runID); err != nil {
		return ReviewCandidate{}, err
	}
	if last := a.runs[len(a.runs)-1]; last.ID != runID {
		return ReviewCandidate{}, conflict(RuleCandidateRun, "run %s cannot pin a revision: it is not the latest run (%s is)", runID, last.ID)
	}
	if sha == "" {
		return ReviewCandidate{}, conflict(RulePinnedSHA, "a revision needs a commit SHA")
	}
	for _, c := range a.candidates {
		if c.SHA == sha {
			return ReviewCandidate{}, conflict(RulePinnedSHA, "commit %s is already pinned", sha)
		}
	}
	c := &ReviewCandidate{TaskID: a.task.ID, RunID: runID, Branch: branch, SHA: sha, CI: CIPending, Source: source}
	a.candidates = append(a.candidates, c)
	a.record(EventRevisionPinned, RevisionPinned{RunID: runID, Branch: branch, SHA: sha, Source: source})
	return *c, nil
}

func (a *TaskAggregate) currentCandidate() *ReviewCandidate {
	if len(a.candidates) == 0 {
		return nil
	}
	return a.candidates[len(a.candidates)-1]
}

// CurrentCandidate returns a copy of the current revision, the most recently
// pinned one.
func (a *TaskAggregate) CurrentCandidate() (ReviewCandidate, bool) {
	if c := a.currentCandidate(); c != nil {
		return *c, true
	}
	return ReviewCandidate{}, false
}

// RecordPushed marks a revision as pushed to the forge. Recording the same
// push again changes nothing.
func (a *TaskAggregate) RecordPushed(sha string) error {
	for _, c := range a.candidates {
		if c.SHA == sha {
			if c.Pushed {
				return nil
			}
			c.Pushed = true
			a.record(EventRevisionPushed, RevisionPushed{SHA: sha})
			return nil
		}
	}
	return &NotFoundError{Kind: "commit", ID: sha}
}

// LastPushed returns the most recently pinned revision that was pushed.
func (a *TaskAggregate) LastPushed() (ReviewCandidate, bool) {
	for i := len(a.candidates) - 1; i >= 0; i-- {
		if a.candidates[i].Pushed {
			return *a.candidates[i], true
		}
	}
	return ReviewCandidate{}, false
}

// RecordCI stores a pipeline result on the candidate of its own commit. A
// result for an earlier commit never changes the current revision.
func (a *TaskAggregate) RecordCI(sha string, state CIState) error {
	switch state {
	case CIPending, CIPassed, CIFailed:
	default:
		return fmt.Errorf("unknown CI state %q", state)
	}
	for _, c := range a.candidates {
		if c.SHA == sha {
			c.CI = state
			a.record(EventCIRecorded, CIRecorded{SHA: sha, State: state})
			return nil
		}
	}
	return &NotFoundError{Kind: "commit", ID: sha}
}

// RecordPR stores the URL of the pull request opened for a prepared revision
// on the candidate of its commit. A candidate has one pull request.
func (a *TaskAggregate) RecordPR(sha, url string) error {
	if url == "" {
		return conflict(RulePinnedSHA, "a pull request needs a URL")
	}
	for _, c := range a.candidates {
		if c.SHA == sha {
			if c.PRURL != "" && c.PRURL != url {
				return conflict(RulePinnedSHA, "commit %s already has the pull request %s", sha, c.PRURL)
			}
			if c.PRURL == url {
				return nil
			}
			c.PRURL = url
			a.record(EventPRRecorded, PRRecorded{SHA: sha, URL: url})
			return nil
		}
	}
	return &NotFoundError{Kind: "commit", ID: sha}
}

// CIPassed reports whether the pipeline passed for the current revision. A pass
// on an earlier commit does not count.
func (a *TaskAggregate) CIPassed() bool {
	c := a.currentCandidate()
	return c != nil && c.CI == CIPassed
}

// MarkReady moves the task to ready_for_review. It requires that the task's
// latest run is stopped (not failed, not live) and that a commit SHA is
// pinned by that run, so after rework the old revision never counts. Where requireCI is set, the current revision must also have passed
// CI: a pass on an earlier SHA is not enough.
func (a *TaskAggregate) MarkReady(requireCI bool) error {
	if len(a.runs) == 0 {
		return conflict(RuleStoppedRun, "task %s has no run", a.task.ID)
	}
	if last := a.runs[len(a.runs)-1]; last.State != RunStopped {
		return conflict(RuleStoppedRun, "task %s is not ready: its latest run %s is %s, not stopped", a.task.ID, last.ID, last.State)
	}
	cur := a.currentCandidate()
	if cur == nil || cur.SHA == "" {
		return conflict(RulePinnedSHA, "task %s is not ready: no commit is pinned", a.task.ID)
	}
	if last := a.runs[len(a.runs)-1]; cur.RunID != last.ID {
		return conflict(RuleCandidateRun, "task %s is not ready: the current commit %s came from run %s, not the latest run %s", a.task.ID, cur.SHA, cur.RunID, last.ID)
	}
	if requireCI && cur.CI != CIPassed {
		msg := fmt.Sprintf("task %s is not ready: CI is %s for the current commit %s", a.task.ID, orPending(cur.CI), cur.SHA)
		for _, c := range a.candidates[:len(a.candidates)-1] {
			if c.CI == CIPassed {
				msg += fmt.Sprintf(" (it passed for the earlier commit %s, which does not count)", c.SHA)
				break
			}
		}
		return conflict(RuleCIPassed, "%s", msg)
	}
	return a.moveTask(TaskReadyForReview)
}

func orPending(s CIState) CIState {
	if s == "" {
		return CIPending
	}
	return s
}
