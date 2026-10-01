package domain

import (
	"strings"
	"testing"
)

// newStoppedAggregate returns a running task whose only run has stopped and
// whose current revision is aaa111.
func newStoppedAggregate(t *testing.T) *TaskAggregate {
	t.Helper()
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	if _, err := a.PinRevision("r1", "agent/topic", "aaa111"); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestMarkReadyNeedsAStoppedRun(t *testing.T) {
	a := NewTaskAggregate(Task{ID: "t1", State: TaskRunning})
	wantConflict(t, a.MarkReady(false), RuleStoppedRun) // no run at all

	for _, state := range []RunState{RunStarting, RunRunning, RunPaused, RunInterrupted, RunFailed} {
		a := newStoppedAggregate(t)
		a.Runs[0].State = state
		wantConflict(t, a.MarkReady(false), RuleStoppedRun)
		if a.Task.State != TaskRunning {
			t.Errorf("run %s: a refused MarkReady changed the task to %s", state, a.Task.State)
		}
	}
}

func TestMarkReadyUsesTheLatestRun(t *testing.T) {
	a := newStoppedAggregate(t)
	// A rework starts a new run; until it stops, the task is not ready, even
	// though an earlier run did stop.
	next := &Run{ID: "r2", EnvID: "e1"}
	if err := a.StartRun(next); err != nil {
		t.Fatal(err)
	}
	wantConflict(t, a.MarkReady(false), RuleStoppedRun)
	next.State = RunStopped
	// The rework run produced no commit of its own: the old revision is not ready.
	wantConflict(t, a.MarkReady(false), RuleCandidateRun)
	if _, err := a.PinRevision("r2", "agent/topic", "bbb222"); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkReady(false); err != nil {
		t.Fatalf("after the latest run stopped and pinned its commit: %v", err)
	}
}

func TestMarkReadyNeedsAPinnedSHA(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	wantConflict(t, a.MarkReady(false), RulePinnedSHA) // nothing pinned

	a.Candidates = append(a.Candidates, &ReviewCandidate{TaskID: "t1", RunID: "r1", Branch: "agent/topic"}) // no SHA
	wantConflict(t, a.MarkReady(false), RulePinnedSHA)
	if a.Task.State != TaskRunning {
		t.Errorf("a refused MarkReady changed the task to %s", a.Task.State)
	}
}

func TestMarkReadyMovesTheTask(t *testing.T) {
	a := newStoppedAggregate(t)
	if err := a.MarkReady(false); err != nil {
		t.Fatal(err)
	}
	if a.Task.State != TaskReadyForReview {
		t.Errorf("task state = %s, want ready_for_review", a.Task.State)
	}
	// The task machine still has the last word.
	queued := newStoppedAggregate(t)
	queued.Task.State = TaskQueued
	wantConflict(t, queued.MarkReady(false), RuleTransition)
}

func TestPinRevision(t *testing.T) {
	a, _, _ := newRunningAggregate(t)
	if a.CurrentCandidate() != nil {
		t.Fatal("no current revision before one is pinned")
	}
	wantConflict(t, errOnly(a.PinRevision("r1", "agent/topic", "")), RulePinnedSHA)
	wantNotFound(t, errOnly(a.PinRevision("nope", "agent/topic", "aaa111")))

	first, err := a.PinRevision("r1", "agent/topic", "aaa111")
	if err != nil {
		t.Fatal(err)
	}
	if first.CI != CIPending || first.TaskID != "t1" || first.Branch != "agent/topic" || first.RunID != "r1" {
		t.Errorf("candidate = %+v", first)
	}
	wantConflict(t, errOnly(a.PinRevision("r1", "agent/topic", "aaa111")), RulePinnedSHA)

	second, err := a.PinRevision("r1", "agent/topic", "bbb222")
	if err != nil {
		t.Fatal(err)
	}
	if a.CurrentCandidate() != second || len(a.Candidates) != 2 {
		t.Errorf("the most recently pinned revision must be current")
	}
}

func errOnly(_ *ReviewCandidate, err error) error { return err }

func TestCIIsOnlyRequiredWhenAsked(t *testing.T) {
	a := newStoppedAggregate(t)
	if err := a.MarkReady(false); err != nil {
		t.Fatalf("without required CI: %v", err)
	}
	for _, state := range []CIState{"", CIPending, CIFailed} {
		a := newStoppedAggregate(t)
		a.CurrentCandidate().CI = state
		wantConflict(t, a.MarkReady(true), RuleCIPassed)
	}
	a = newStoppedAggregate(t)
	if err := a.RecordCI("aaa111", CIPassed); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkReady(true); err != nil {
		t.Fatalf("CI passed for the current commit: %v", err)
	}
}

// A passing pipeline on an earlier SHA never marks the current revision ready.
func TestPipelineOnAnEarlierSHADoesNotCount(t *testing.T) {
	a := newStoppedAggregate(t)
	if err := a.RecordCI("aaa111", CIPassed); err != nil {
		t.Fatal(err)
	}
	if !a.CIPassed() {
		t.Fatal("CI passed for the only revision")
	}

	// The topic gains a commit: the current revision is now bbb222.
	if _, err := a.PinRevision("r1", "agent/topic", "bbb222"); err != nil {
		t.Fatal(err)
	}
	if a.CIPassed() {
		t.Error("a pass on aaa111 must not count for the current revision bbb222")
	}
	err := a.MarkReady(true)
	wantConflict(t, err, RuleCIPassed)
	if msg := err.Error(); !strings.Contains(msg, "aaa111") || !strings.Contains(msg, "bbb222") {
		t.Errorf("message %q should name both commits", msg)
	}

	// A late or repeated result for the earlier commit changes nothing for the new one.
	if err := a.RecordCI("aaa111", CIPassed); err != nil {
		t.Fatal(err)
	}
	if got := a.CurrentCandidate().CI; got != CIPending {
		t.Errorf("current revision CI = %q after a result for an earlier commit, want pending", got)
	}
	wantConflict(t, a.MarkReady(true), RuleCIPassed)
	if a.Task.State != TaskRunning {
		t.Errorf("a refused MarkReady changed the task to %s", a.Task.State)
	}

	// Only its own pipeline marks the current revision ready.
	if err := a.RecordCI("bbb222", CIPassed); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkReady(true); err != nil {
		t.Fatalf("after CI passed for bbb222: %v", err)
	}
}

func TestRecordCI(t *testing.T) {
	a := newStoppedAggregate(t)
	wantNotFound(t, a.RecordCI("zzz999", CIPassed))
	if err := a.RecordCI("aaa111", "flaky"); err == nil {
		t.Error("an unknown CI state must be refused")
	}
	if got := a.CurrentCandidate().CI; got != CIPending {
		t.Errorf("a refused result changed CI to %q", got)
	}
	if err := a.RecordCI("aaa111", CIFailed); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordCI("aaa111", CIPassed); err != nil { // a re-run may pass
		t.Fatal(err)
	}
	if !a.CIPassed() {
		t.Error("the re-run result must replace the failure")
	}
}

// #52: after rework a new run that stops without a new pin let MarkReady
// succeed on the old SHA and its old CI pass.
func TestReworkNeverReusesTheOldRevisionOrItsCI(t *testing.T) {
	a := newStoppedAggregate(t)
	if err := a.RecordCI("aaa111", CIPassed); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkReady(true); err != nil {
		t.Fatal(err)
	}
	if a.CurrentCandidate().RunID != "r1" {
		t.Fatalf("the candidate records run %q, want r1", a.CurrentCandidate().RunID)
	}

	// Rework: the push was declined, the task runs again with a new run.
	a.Task.State = TaskRunning
	rework := &Run{ID: "r2", EnvID: "e1"}
	if err := a.StartRun(rework); err != nil {
		t.Fatal(err)
	}
	rework.State = RunStopped // it changed nothing, so it pinned nothing

	err := a.MarkReady(true)
	wantConflict(t, err, RuleCandidateRun)
	if a.Task.State != TaskRunning {
		t.Errorf("a refused MarkReady changed the task to %s", a.Task.State)
	}
	if !strings.Contains(err.Error(), "r1") || !strings.Contains(err.Error(), "r2") {
		t.Errorf("message %q should name both runs", err)
	}

	// Its own commit and its own CI result make the task ready again.
	if _, err := a.PinRevision("r2", "agent/topic", "bbb222"); err != nil {
		t.Fatal(err)
	}
	wantConflict(t, a.MarkReady(true), RuleCIPassed)
	if err := a.RecordCI("bbb222", CIPassed); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkReady(true); err != nil {
		t.Fatalf("after the rework run pinned and passed: %v", err)
	}
}
