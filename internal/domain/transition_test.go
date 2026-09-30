package domain

import "testing"

func TestTaskTransition(t *testing.T) {
	task := &Task{ID: "t1", State: TaskQueued}
	if err := task.Transition(TaskRunning); err != nil {
		t.Fatal(err)
	}
	if err := task.Transition(TaskCompleted); err == nil {
		t.Fatal("running -> completed must be illegal")
	}
	if err := task.Transition(TaskReadyForReview); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalStatesHaveNoTransitions(t *testing.T) {
	for _, s := range []TaskState{TaskCompleted, TaskCancelled} {
		if s.CanTransition(TaskRunning) {
			t.Errorf("%s must be terminal", s)
		}
	}
}
