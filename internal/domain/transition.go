package domain

import "fmt"

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
	for _, next := range taskTransitions[s] {
		if next == to {
			return true
		}
	}
	return false
}

// Terminal reports whether no transition leaves the state.
func (s TaskState) Terminal() bool {
	return s == TaskCompleted || s == TaskCancelled || s == TaskFailed
}

// Transition validates a task state change.
func (t *Task) Transition(to TaskState) error {
	if !t.State.CanTransition(to) {
		return fmt.Errorf("task %s: illegal transition %s -> %s", t.ID, t.State, to)
	}
	t.State = to
	return nil
}
