package domain

import (
	"errors"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

// newRunningAggregate returns a task with a running run in a running
// environment.
func newRunningAggregate(t *testing.T) (*TaskAggregate, *Run, *Environment) {
	t.Helper()
	a := NewTaskAggregate(Task{ID: "t1", State: TaskRunning})
	env := &Environment{ID: "e1", Backend: "apple", State: EnvRunning}
	a.AddEnvironment(env)
	run := &Run{ID: "r1", EnvID: "e1"}
	if err := a.StartRun(run); err != nil {
		t.Fatal(err)
	}
	if err := run.Transition(RunRunning); err != nil {
		t.Fatal(err)
	}
	return a, run, env
}

// wantConflict checks that err is a conflict of the given rule that exits with
// Conflict.
func wantConflict(t *testing.T, err error, rule Rule) {
	t.Helper()
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %v, want a *ConflictError for rule %q", err, rule)
	}
	if ce.Rule != rule {
		t.Errorf("rule = %q, want %q (%v)", ce.Rule, rule, err)
	}
	if got := exitcode.From(err); got != exitcode.Conflict {
		t.Errorf("exit code = %d, want Conflict (%d)", got, exitcode.Conflict)
	}
}

func wantNotFound(t *testing.T, err error) {
	t.Helper()
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("error = %v, want a *NotFoundError", err)
	}
	if got := exitcode.From(err); got != exitcode.NotFound {
		t.Errorf("exit code = %d, want NotFound (%d)", got, exitcode.NotFound)
	}
}

func TestStartRunNeedsARunningEnvironment(t *testing.T) {
	for _, state := range []EnvState{EnvProvisioning, EnvStopped, EnvDeleted} {
		a := NewTaskAggregate(Task{ID: "t1", State: TaskRunning})
		a.AddEnvironment(&Environment{ID: "e1", State: state})
		err := a.StartRun(&Run{ID: "r1", EnvID: "e1"})
		wantConflict(t, err, RuleEnvRunning)
		if len(a.Runs) != 0 {
			t.Errorf("environment %s: a refused run was added", state)
		}
	}
	a := NewTaskAggregate(Task{ID: "t1", State: TaskRunning})
	wantNotFound(t, a.StartRun(&Run{ID: "r1", EnvID: "missing"}))
}

func TestStartRunSetsTaskAndState(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	if run.TaskID != "t1" {
		t.Errorf("TaskID = %q, want t1", run.TaskID)
	}
	if len(a.Runs) != 1 || a.LiveRun() != run {
		t.Errorf("runs = %v, live = %v", a.Runs, a.LiveRun())
	}
}

func TestOneLiveRunPerTask(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	wantConflict(t, a.StartRun(&Run{ID: "r2", EnvID: "e1"}), RuleOneLiveRun)

	// Every non-terminal state still counts as live.
	for _, state := range []RunState{RunStarting, RunRunning, RunPaused, RunInterrupted} {
		run.State = state
		if a.LiveRun() == nil {
			t.Errorf("a %s run must be live", state)
		}
		wantConflict(t, a.StartRun(&Run{ID: "rx", EnvID: "e1"}), RuleOneLiveRun)
	}

	// Once the run is over, a new one may start; the old one is never reused.
	for _, state := range []RunState{RunStopped, RunFailed} {
		a, run, _ := newRunningAggregate(t)
		run.State = state
		if a.LiveRun() != nil {
			t.Errorf("a %s run is not live", state)
		}
		next := &Run{ID: "r2", EnvID: "e1"}
		if err := a.StartRun(next); err != nil {
			t.Errorf("after a %s run: %v", state, err)
		}
		if len(a.Runs) != 2 || next.State != RunStarting {
			t.Errorf("after a %s run: runs %d, state %s", state, len(a.Runs), next.State)
		}
	}
}

// Pausing a run never stops its environment.
func TestPauseNeverStopsTheEnvironment(t *testing.T) {
	a, run, env := newRunningAggregate(t)
	if err := a.Pause("r1"); err != nil {
		t.Fatal(err)
	}
	if run.State != RunPaused {
		t.Errorf("run state = %s, want paused", run.State)
	}
	if env.State != EnvRunning {
		t.Errorf("environment state = %s after pause, want running", env.State)
	}
	if a.Envs["e1"] != env {
		t.Error("pause replaced the environment")
	}
	// Only a running run can be paused.
	wantConflict(t, a.Pause("r1"), RuleTransition)
	wantNotFound(t, a.Pause("nope"))
}

func TestPauseNeedsTheEnvironmentRunning(t *testing.T) {
	a, _, env := newRunningAggregate(t)
	env.State = EnvStopped // inconsistent: the reconciler should interrupt the run
	wantConflict(t, a.Pause("r1"), RuleEnvRunning)
}

func TestEnvironmentIsNotStoppedUnderAnActiveRun(t *testing.T) {
	for _, state := range []RunState{RunStarting, RunRunning} {
		a, run, env := newRunningAggregate(t)
		run.State = state
		wantConflict(t, a.StopEnvironment("e1"), RuleEnvInUse)
		if env.State != EnvRunning {
			t.Errorf("run %s: a refused stop changed the environment to %s", state, env.State)
		}
	}
	for _, state := range []RunState{RunPaused, RunInterrupted, RunStopped, RunFailed} {
		a, run, env := newRunningAggregate(t)
		run.State = state
		if err := a.StopEnvironment("e1"); err != nil {
			t.Errorf("run %s: %v", state, err)
		}
		if env.State != EnvStopped {
			t.Errorf("run %s: environment state = %s, want stopped", state, env.State)
		}
	}
	a, _, _ := newRunningAggregate(t)
	wantNotFound(t, a.StopEnvironment("missing"))
}

func TestResumeNeedsARunningEnvironment(t *testing.T) {
	for _, from := range []RunState{RunPaused, RunInterrupted} {
		a, run, env := newRunningAggregate(t)
		run.State = from
		env.State = EnvStopped
		wantConflict(t, a.Resume("r1"), RuleEnvRunning)
		if run.State != from {
			t.Errorf("%s: a refused resume changed the run to %s", from, run.State)
		}

		if err := env.Transition(EnvRunning); err != nil {
			t.Fatal(err)
		}
		if err := a.Resume("r1"); err != nil {
			t.Fatalf("%s: resume in a running environment: %v", from, err)
		}
		if run.State != RunStarting {
			t.Errorf("%s: run state = %s, want starting", from, run.State)
		}
	}
}

func TestResumeOfARunThatCannotResume(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	// A running run cannot be resumed, and a stopped one never again.
	wantConflict(t, a.Resume("r1"), RuleTransition)
	run.State = RunStopped
	wantConflict(t, a.Resume("r1"), RuleTransition)
	wantNotFound(t, a.Resume("nope"))
}

// #52: a stopped run passed back in returned to starting and was appended twice.
func TestStartRunAcceptsOnlyAnUnusedRun(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	wantConflict(t, a.StartRun(run), RuleRunReused)
	if run.State != RunStopped || len(a.Runs) != 1 {
		t.Errorf("a refused run was changed or added: state %s, %d runs", run.State, len(a.Runs))
	}

	for _, state := range allRunStates {
		next := &Run{ID: ID("r-" + string(state)), EnvID: "e1", State: state}
		wantConflict(t, a.StartRun(next), RuleRunReused)
	}
	if len(a.Runs) != 1 {
		t.Errorf("%d runs after refused starts, want 1", len(a.Runs))
	}
}

func TestStartRunNeedsAUniqueNonEmptyID(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped

	wantConflict(t, a.StartRun(&Run{ID: "", EnvID: "e1"}), RuleRunID)
	wantConflict(t, a.StartRun(&Run{ID: "r1", EnvID: "e1"}), RuleRunID) // the stopped run's ID
	if len(a.Runs) != 1 {
		t.Fatalf("%d runs after refused starts, want 1", len(a.Runs))
	}
	if got, err := a.run("r1"); err != nil || got != run {
		t.Errorf("run(r1) = %v, %v; want the first run", got, err)
	}

	fresh := &Run{ID: "r2", EnvID: "e1"}
	if err := a.StartRun(fresh); err != nil {
		t.Fatalf("a new run with a new ID: %v", err)
	}
	if fresh.State != RunStarting || len(a.Runs) != 2 {
		t.Errorf("state %s, %d runs", fresh.State, len(a.Runs))
	}
}

// #52: a run started on a completed, cancelled or failed task without error.
func TestStartRunOnlyWhereTheTaskCanTakeOne(t *testing.T) {
	for _, state := range allTaskStates {
		a := NewTaskAggregate(Task{ID: "t1", State: state})
		a.AddEnvironment(&Environment{ID: "e1", State: EnvRunning})
		err := a.StartRun(&Run{ID: "r1", EnvID: "e1"})
		switch state {
		case TaskQueued, TaskRunning, TaskReadyForReview: // ready_for_review is rework
			if err != nil {
				t.Errorf("task %s: %v", state, err)
			}
		default:
			wantConflict(t, err, RuleTaskState)
			if len(a.Runs) != 0 {
				t.Errorf("task %s: a refused run was added", state)
			}
		}
	}
}
