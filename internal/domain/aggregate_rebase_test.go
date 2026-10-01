package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRebaseConflictIsAQuestionForTheStoppedRun(t *testing.T) {
	a := newStoppedAggregate(t)
	d, err := a.RaiseRebaseConflict("r1", "d-conflict", "main", []string{"docs/a.md", "cmd/x.go"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != DecisionQuestion || !d.Blocking || d.Cause != CauseRebaseConflict || d.RunID != "r1" {
		t.Errorf("decision %+v", d)
	}
	if got := strings.Join(d.Options, ","); got != "rework,retry,cancel" {
		t.Errorf("options = %s", got)
	}
	if d.Input != "docs/a.md\ncmd/x.go" {
		t.Errorf("the paths are the input: %q", d.Input)
	}
	if a.task.State != TaskAwaitingGuidance {
		t.Errorf("the task is %s, want awaiting_guidance", a.task.State)
	}
	if a.mustRun("r1").State != RunStopped {
		t.Error("the run must stay stopped: the state machines do not change")
	}

	// A second one with the same ID is refused; retry frees the task again.
	if _, err := a.RaiseRebaseConflict("r1", "d-conflict", "main", nil, time.Now()); err == nil {
		t.Error("a duplicate decision ID was accepted")
	}
	if err := a.Answer("d-conflict", Response{Option: AnswerRetry, By: "human", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskRunning {
		t.Errorf("after retry the task is %s, want running", a.task.State)
	}
}

func TestRebaseConflictCancelCancelsTheTask(t *testing.T) {
	a := newStoppedAggregate(t)
	if _, err := a.RaiseRebaseConflict("r1", "d1", "main", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.Answer("d1", Response{Option: AnswerCancel, By: "human", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskCancelled {
		t.Errorf("task = %s", a.task.State)
	}
}

func TestRebaseConflictNeedsAStoppedRun(t *testing.T) {
	for _, state := range []RunState{RunStarting, RunRunning, RunPaused, RunInterrupted, RunFailed} {
		a := newStoppedAggregate(t)
		a.runs[0].State = state
		var c *ConflictError
		if _, err := a.RaiseRebaseConflict("r1", "d1", "main", nil, time.Now()); !errors.As(err, &c) || c.Rule != RuleRunLive {
			t.Errorf("a %s run: %v", state, err)
		}
	}
	a := newStoppedAggregate(t)
	if _, err := a.RaiseRebaseConflict("nope", "d1", "main", nil, time.Now()); err == nil {
		t.Error("an unknown run was accepted")
	}
}

func TestRebaseConflictPathsAreCappedUntrustedInput(t *testing.T) {
	a := newStoppedAggregate(t)
	huge := []string{strings.Repeat("x", 5000)}
	d, err := a.RaiseRebaseConflict("r1", "d1", "main", huge, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Input) > MaxDecisionInput || !d.InputTruncated {
		t.Errorf("input of %d characters, truncated=%v", len(d.Input), d.InputTruncated)
	}
}

func TestRunFailedAgainOnlyForAFailedRun(t *testing.T) {
	a := newStoppedAggregate(t)
	a.runs[0].State = RunFailed
	d, err := a.RaiseRunFailedAgain("r1", "d-again", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Cause != CauseRunFailed || !d.Blocking || strings.Join(d.Options, ",") != "retry,cancel" || a.task.State != TaskAwaitingGuidance {
		t.Errorf("decision %+v, task %s", d, a.task.State)
	}
	if _, err := a.RaiseRunFailedAgain("r1", "d-again", time.Now()); err == nil {
		t.Error("a duplicate decision ID was accepted")
	}
	for _, state := range []RunState{RunStarting, RunRunning, RunPaused, RunInterrupted, RunStopped} {
		b := newStoppedAggregate(t)
		b.runs[0].State = state
		var c *ConflictError
		if _, err := b.RaiseRunFailedAgain("r1", "d1", time.Now()); !errors.As(err, &c) {
			t.Errorf("a %s run: %v", state, err)
		}
	}
}
