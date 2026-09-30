package domain

import "fmt"

var taskTransitions = map[TaskState][]TaskState{
	TaskQueued:           {TaskRunning, TaskCancelled},
	TaskRunning:          {TaskAwaitingGuidance, TaskReadyForReview, TaskCancelled},
	TaskAwaitingGuidance: {TaskRunning, TaskCancelled},
	TaskReadyForReview:   {TaskRunning, TaskCompleted, TaskCancelled},
}

// CanTransition reports whether a task may move from one state to another.
func (from TaskState) CanTransition(to TaskState) bool {
	for _, s := range taskTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Transition validates a task state change.
func (t *Task) Transition(to TaskState) error {
	if !t.State.CanTransition(to) {
		return fmt.Errorf("task %s: illegal transition %s -> %s", t.ID, t.State, to)
	}
	t.State = to
	return nil
}
