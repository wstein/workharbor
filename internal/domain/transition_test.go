package domain

import "testing"

var allTaskStates = []TaskState{
	TaskQueued,
	TaskRunning,
	TaskAwaitingGuidance,
	TaskReadyForReview,
	TaskCompleted,
	TaskCancelled,
	TaskFailed,
}

// legalTask is the task state machine of design §4.1, written out in full.
var legalTask = map[[2]TaskState]bool{
	{TaskQueued, TaskRunning}:   true,
	{TaskQueued, TaskCancelled}: true,

	{TaskRunning, TaskAwaitingGuidance}: true,
	{TaskRunning, TaskReadyForReview}:   true,
	{TaskRunning, TaskFailed}:           true,
	{TaskRunning, TaskCancelled}:        true,

	{TaskAwaitingGuidance, TaskRunning}:   true,
	{TaskAwaitingGuidance, TaskFailed}:    true,
	{TaskAwaitingGuidance, TaskCancelled}: true,

	{TaskReadyForReview, TaskRunning}:   true, // rework: push declined or changes requested
	{TaskReadyForReview, TaskCompleted}: true,
	{TaskReadyForReview, TaskCancelled}: true,
}

func TestTaskTransitionsExhaustive(t *testing.T) {
	for _, from := range allTaskStates {
		for _, to := range allTaskStates {
			want := legalTask[[2]TaskState{from, to}]
			if got := from.CanTransition(to); got != want {
				t.Errorf("%s -> %s: CanTransition = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTerminalTaskStates(t *testing.T) {
	for _, s := range allTaskStates {
		terminal := s == TaskCompleted || s == TaskCancelled || s == TaskFailed
		if got := s.Terminal(); got != terminal {
			t.Errorf("%s: Terminal = %v, want %v", s, got, terminal)
		}
	}
}

func TestTaskTransition(t *testing.T) {
	task := &Task{ID: "t1", State: TaskQueued}
	if err := task.transition(TaskRunning); err != nil {
		t.Fatal(err)
	}
	if err := task.transition(TaskCompleted); err == nil {
		t.Fatal("running -> completed must be illegal")
	}
	if task.State != TaskRunning {
		t.Fatalf("state after illegal transition = %s, want running", task.State)
	}
	if err := task.transition(TaskReadyForReview); err != nil {
		t.Fatal(err)
	}
}
