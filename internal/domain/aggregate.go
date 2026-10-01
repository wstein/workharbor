package domain

import "fmt"

// Rules of design §4.1 that couple the task, run and environment machines.
const (
	RuleOneLiveRun Rule = "one-live-run" // a task has at most one run that is not stopped or failed
	RuleRunReused  Rule = "run-reused"   // StartRun takes a run that has not started
	RuleRunID      Rule = "run-id"       // a run has a non-empty ID that is unique in the task
	RuleTaskState  Rule = "task-state"   // a run starts only on a queued, running or ready_for_review task
	RuleEnvRunning Rule = "env-running"  // a run is created or started only in a running environment
	RuleEnvInUse   Rule = "env-in-use"   // an environment is not stopped under a starting or running run
	RuleStoppedRun Rule = "stopped-run"  // ready_for_review needs the task's latest run to be stopped
	RulePinnedSHA  Rule = "pinned-sha"   // ready_for_review needs a pinned commit
	RuleCIPassed   Rule = "ci-passed"    // where CI is required, the current commit must have passed
)

// TaskAggregate is a task with the runs, environments and review candidates
// it is coupled to. The task, run and environment machines are each valid on
// their own; the methods here are the guards that keep them consistent with
// one another (design §4.1). A violation is a *ConflictError, which maps to
// exit code Conflict; an unknown run or environment is a *NotFoundError.
type TaskAggregate struct {
	Task Task
	Runs []*Run // oldest first
	Envs map[ID]*Environment

	// Candidates are the prepared revisions, oldest first. The last one is the
	// current revision.
	Candidates []*ReviewCandidate
}

// NewTaskAggregate returns an aggregate for task with nothing attached yet.
func NewTaskAggregate(task Task) *TaskAggregate {
	return &TaskAggregate{Task: task, Envs: map[ID]*Environment{}}
}

// AddEnvironment registers an environment the task's runs may use.
func (a *TaskAggregate) AddEnvironment(e *Environment) { a.Envs[e.ID] = e }

func (a *TaskAggregate) run(id ID) (*Run, error) {
	for _, r := range a.Runs {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, &NotFoundError{Kind: "run", ID: string(id)}
}

func (a *TaskAggregate) env(id ID) (*Environment, error) {
	if e, ok := a.Envs[id]; ok {
		return e, nil
	}
	return nil, &NotFoundError{Kind: "environment", ID: string(id)}
}

// LiveRun returns the run that is not yet stopped or failed, or nil.
func (a *TaskAggregate) LiveRun() *Run {
	for _, r := range a.Runs {
		if !r.State.Terminal() {
			return r
		}
	}
	return nil
}

// StartRun adds a new run in an environment and starts it. The run must be new
// (no state yet) with an ID no run of the task has. A task has at most one live
// run, and the environment must be running.
func (a *TaskAggregate) StartRun(run *Run) error {
	if run.State != "" {
		return conflict(RuleRunReused, "run %s is already %s: a new run is started, a finished one is never reused", run.ID, run.State)
	}
	if run.ID == "" {
		return conflict(RuleRunID, "a run needs an ID")
	}
	for _, r := range a.Runs {
		if r.ID == run.ID {
			return conflict(RuleRunID, "run %s already exists in task %s", run.ID, a.Task.ID)
		}
	}
	switch a.Task.State {
	case TaskQueued, TaskRunning, TaskReadyForReview: // the last is rework
	default:
		return conflict(RuleTaskState, "task %s is %s and starts no run", a.Task.ID, a.Task.State)
	}
	if live := a.LiveRun(); live != nil {
		return conflict(RuleOneLiveRun, "task %s already has a live run %s (%s)", a.Task.ID, live.ID, live.State)
	}
	env, err := a.env(run.EnvID)
	if err != nil {
		return err
	}
	if env.State != EnvRunning {
		return conflict(RuleEnvRunning, "run %s needs a running environment, but %s is %s", run.ID, env.ID, env.State)
	}
	run.TaskID = a.Task.ID
	run.State = RunStarting
	a.Runs = append(a.Runs, run)
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
	return run.Transition(RunPaused)
}

// Resume starts a paused or interrupted run again, which relaunches the agent
// from its session. The environment must be running.
func (a *TaskAggregate) Resume(runID ID) error {
	run, err := a.run(runID)
	if err != nil {
		return err
	}
	if !run.State.CanTransition(RunStarting) {
		return run.Transition(RunStarting) // reports the illegal transition
	}
	env, err := a.env(run.EnvID)
	if err != nil {
		return err
	}
	if env.State != EnvRunning {
		return conflict(RuleEnvRunning, "run %s cannot resume: its environment %s is %s", run.ID, env.ID, env.State)
	}
	return run.Transition(RunStarting)
}

// StopEnvironment stops an environment. It is refused while a run in it is
// starting or running: the run is stopped or interrupted first.
func (a *TaskAggregate) StopEnvironment(envID ID) error {
	env, err := a.env(envID)
	if err != nil {
		return err
	}
	for _, r := range a.Runs {
		if r.EnvID == envID && (r.State == RunStarting || r.State == RunRunning) {
			return conflict(RuleEnvInUse, "environment %s cannot be stopped: run %s is %s", envID, r.ID, r.State)
		}
	}
	return env.Transition(EnvStopped)
}

// PinRevision records a prepared revision: the commit SHA that cleanup pinned
// on a branch, before it is pushed (design §4.5). It becomes the current
// revision. A SHA is pinned once.
func (a *TaskAggregate) PinRevision(branch, sha string) (*ReviewCandidate, error) {
	if sha == "" {
		return nil, conflict(RulePinnedSHA, "a revision needs a commit SHA")
	}
	for _, c := range a.Candidates {
		if c.SHA == sha {
			return nil, conflict(RulePinnedSHA, "commit %s is already pinned", sha)
		}
	}
	c := &ReviewCandidate{TaskID: a.Task.ID, Branch: branch, SHA: sha, CI: CIPending}
	a.Candidates = append(a.Candidates, c)
	return c, nil
}

// CurrentCandidate returns the current revision, the most recently pinned one,
// or nil.
func (a *TaskAggregate) CurrentCandidate() *ReviewCandidate {
	if len(a.Candidates) == 0 {
		return nil
	}
	return a.Candidates[len(a.Candidates)-1]
}

// RecordCI stores a pipeline result on the candidate of its own commit. A
// result for an earlier commit never changes the current revision.
func (a *TaskAggregate) RecordCI(sha string, state CIState) error {
	switch state {
	case CIPending, CIPassed, CIFailed:
	default:
		return fmt.Errorf("unknown CI state %q", state)
	}
	for _, c := range a.Candidates {
		if c.SHA == sha {
			c.CI = state
			return nil
		}
	}
	return &NotFoundError{Kind: "commit", ID: sha}
}

// CIPassed reports whether the pipeline passed for the current revision. A pass
// on an earlier commit does not count.
func (a *TaskAggregate) CIPassed() bool {
	c := a.CurrentCandidate()
	return c != nil && c.CI == CIPassed
}

// MarkReady moves the task to ready_for_review. It requires that the task's
// latest run is stopped (not failed, not live) and that a commit SHA is
// pinned. Where requireCI is set, the current revision must also have passed
// CI: a pass on an earlier SHA is not enough.
func (a *TaskAggregate) MarkReady(requireCI bool) error {
	if len(a.Runs) == 0 {
		return conflict(RuleStoppedRun, "task %s has no run", a.Task.ID)
	}
	if last := a.Runs[len(a.Runs)-1]; last.State != RunStopped {
		return conflict(RuleStoppedRun, "task %s is not ready: its latest run %s is %s, not stopped", a.Task.ID, last.ID, last.State)
	}
	cur := a.CurrentCandidate()
	if cur == nil || cur.SHA == "" {
		return conflict(RulePinnedSHA, "task %s is not ready: no commit is pinned", a.Task.ID)
	}
	if requireCI && cur.CI != CIPassed {
		msg := fmt.Sprintf("task %s is not ready: CI is %s for the current commit %s", a.Task.ID, orPending(cur.CI), cur.SHA)
		for _, c := range a.Candidates[:len(a.Candidates)-1] {
			if c.CI == CIPassed {
				msg += fmt.Sprintf(" (it passed for the earlier commit %s, which does not count)", c.SHA)
				break
			}
		}
		return conflict(RuleCIPassed, "%s", msg)
	}
	return a.Task.Transition(TaskReadyForReview)
}

func orPending(s CIState) CIState {
	if s == "" {
		return CIPending
	}
	return s
}
