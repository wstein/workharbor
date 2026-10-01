package domain

// taskTransitions is the task state machine of design §4.1. Completed,
// cancelled and failed are terminal.
var taskTransitions = map[TaskState][]TaskState{
	TaskQueued:           {TaskRunning, TaskCancelled},
	TaskRunning:          {TaskAwaitingGuidance, TaskReadyForReview, TaskFailed, TaskCancelled},
	TaskAwaitingGuidance: {TaskRunning, TaskFailed, TaskCancelled},
	TaskReadyForReview:   {TaskRunning, TaskCompleted, TaskCancelled},
}

// CanTransition reports whether a task may move from one state to another.
func (s TaskState) CanTransition(to TaskState) bool {
	return can(taskTransitions, s, to)
}

// Terminal reports whether no transition leaves the state.
func (s TaskState) Terminal() bool {
	return s == TaskCompleted || s == TaskCancelled || s == TaskFailed
}

// Transition validates a task state change.
func (t *Task) Transition(to TaskState) error {
	if !t.State.CanTransition(to) {
		return conflict(RuleTransition, "task %s: illegal transition %s -> %s", t.ID, t.State, to)
	}
	t.State = to
	return nil
}

// runTransitions is the run state machine of design §4.1. Pause is a run
// state. Under D11 pause is a hard interrupt, so a paused run resumes by
// relaunching the agent (starting), which can fail; running directly is for
// an agent with cooperative pause. A run enters interrupted only when the
// reconciler finds its process or environment gone, and leaves it by resuming
// (starting), being cancelled (stopped) or giving up (failed). Stopped and
// failed are terminal: a retry or rework is a new run.
var runTransitions = map[RunState][]RunState{
	RunStarting:    {RunRunning, RunStopped, RunFailed, RunInterrupted},
	RunRunning:     {RunPaused, RunStopped, RunFailed, RunInterrupted},
	RunPaused:      {RunStarting, RunRunning, RunStopped, RunInterrupted},
	RunInterrupted: {RunStarting, RunStopped, RunFailed},
}

// CanTransition reports whether a run may move from one state to another.
func (s RunState) CanTransition(to RunState) bool {
	return can(runTransitions, s, to)
}

// Terminal reports whether no transition leaves the state.
func (s RunState) Terminal() bool {
	return s == RunStopped || s == RunFailed
}

// Transition validates a run state change.
func (r *Run) Transition(to RunState) error {
	if !r.State.CanTransition(to) {
		return conflict(RuleTransition, "run %s: illegal transition %s -> %s", r.ID, r.State, to)
	}
	r.State = to
	return nil
}

// envTransitions is the environment state machine of design §4.1. An
// environment is only deleted once stopped, and never reprovisioned:
// recycling creates a new environment.
var envTransitions = map[EnvState][]EnvState{
	EnvProvisioning: {EnvStopped, EnvDeleted},
	EnvStopped:      {EnvRunning, EnvDeleted},
	EnvRunning:      {EnvStopped},
}

// CanTransition reports whether an environment may move from one state to
// another.
func (s EnvState) CanTransition(to EnvState) bool {
	return can(envTransitions, s, to)
}

// Terminal reports whether no transition leaves the state.
func (s EnvState) Terminal() bool {
	return s == EnvDeleted
}

// Transition validates an environment state change.
func (e *Environment) Transition(to EnvState) error {
	if !e.State.CanTransition(to) {
		return conflict(RuleTransition, "environment %s: illegal transition %s -> %s", e.ID, e.State, to)
	}
	e.State = to
	return nil
}
