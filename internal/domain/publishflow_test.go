package domain

import (
	"strings"
	"testing"
	"time"
)

// newUnpreparedAggregate is a running task whose only run has stopped and
// nothing is pinned for it yet.
func newUnpreparedAggregate(t *testing.T) *TaskAggregate {
	t.Helper()
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	return a
}

func TestPrepareFailedIsAQuestionForTheStoppedRun(t *testing.T) {
	a := newUnpreparedAggregate(t)
	d, err := a.RaisePrepareFailed("r1", "d-prep", "the check exited with status 1\n\nFAIL", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != DecisionQuestion || !d.Blocking || d.Cause != CausePrepareFailed || d.RunID != "r1" {
		t.Errorf("decision %+v", d)
	}
	if got := strings.Join(d.Options, ","); got != "rework,retry,cancel" {
		t.Errorf("options = %s", got)
	}
	if a.task.State != TaskAwaitingGuidance {
		t.Errorf("task = %s, want awaiting_guidance", a.task.State)
	}
	if _, ok := a.PreparePending(); ok {
		t.Error("an open question means nothing is pending")
	}
	// retry frees the task: the reconciler prepares again.
	if err := a.Answer("d-prep", Response{Option: AnswerRetry, By: "human", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if run, ok := a.PreparePending(); !ok || run.ID != "r1" {
		t.Errorf("after retry the prepare is pending again: %v %v", run, ok)
	}
}

func TestPrepareFailedCancelCancelsTheTask(t *testing.T) {
	a := newUnpreparedAggregate(t)
	if _, err := a.RaisePrepareFailed("r1", "d1", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.Answer("d1", Response{Option: AnswerCancel, By: "human", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskCancelled {
		t.Errorf("task = %s", a.task.State)
	}
}

func TestPrepareFailedNeedsTheStoppedLatestRun(t *testing.T) {
	a := newUnpreparedAggregate(t)
	a.runs[0].State = RunRunning
	wantConflict(t, errOf(a.RaisePrepareFailed("r1", "d1", "x", time.Now())), RuleRunLive)
	a = newUnpreparedAggregate(t)
	if err := a.StartRun(Run{ID: "r2", EnvID: "e1"}); err != nil {
		t.Fatal(err)
	}
	wantConflict(t, errOf(a.RaisePrepareFailed("r1", "d1", "x", time.Now())), RuleCandidateRun)
}

func errOf(_ Decision, err error) error { return err }

func TestPreparePendingNeedsAStoppedRunWithNothingPinned(t *testing.T) {
	a := newUnpreparedAggregate(t)
	if run, ok := a.PreparePending(); !ok || run.ID != "r1" {
		t.Fatalf("a stopped run with nothing pinned is pending: %v %v", run, ok)
	}
	if _, err := a.PinRevision("r1", "agent/topic", "aaa111"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.PreparePending(); ok {
		t.Error("a pinned revision for the run ends the pending prepare")
	}
	a, run, _ := newRunningAggregate(t)
	if _, ok := a.PreparePending(); ok {
		t.Errorf("a %s run is not pending", run.State)
	}
}

// readyWithApproval is a task in ready_for_review whose review Decision is
// answered with the given option.
func readyWithApproval(t *testing.T, option string) *TaskAggregate {
	t.Helper()
	a := newStoppedAggregate(t)
	if err := a.MarkReady(false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RaiseDecision(NewDecision{ID: "rev1", Kind: DecisionReview, Blocking: true, SHA: "aaa111", Subject: "Ready to push?", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if option != "" {
		if err := a.Answer("rev1", Response{Option: option, By: "human", SHA: "aaa111", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestOutstandingPublish(t *testing.T) {
	if _, _, ok := readyWithApproval(t, "").OutstandingPublish(); ok {
		t.Error("an unanswered review is no outstanding publish")
	}
	if _, _, ok := readyWithApproval(t, AnswerDeny).OutstandingPublish(); ok {
		t.Error("a denial is no outstanding publish")
	}
	a := readyWithApproval(t, AnswerAllow)
	d, c, ok := a.OutstandingPublish()
	if !ok || d.ID != "rev1" || c.SHA != "aaa111" {
		t.Fatalf("an allowed, unpushed, current revision is outstanding: %v %v %v", d, c, ok)
	}
	if err := a.RecordPushed("aaa111"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.OutstandingPublish(); ok {
		t.Error("a pushed revision is not outstanding")
	}
}

func TestOutstandingPublishEndsWithANewerRevisionOrACancel(t *testing.T) {
	a := readyWithApproval(t, AnswerAllow)
	if err := a.StartRun(Run{ID: "r2", EnvID: "e1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.OutstandingPublish(); ok {
		t.Error("a task running again is not ready_for_review")
	}
	a.mustRun("r2").State = RunStopped
	if _, err := a.PinRevision("r2", "agent/topic", "bbb222"); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkReady(false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.OutstandingPublish(); ok {
		t.Error("an approval for an earlier revision does not cover the newer one")
	}
	b := readyWithApproval(t, AnswerAllow)
	if err := b.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := b.OutstandingPublish(); ok {
		t.Error("a cancelled task has nothing outstanding")
	}
}

func TestPublishFailedKeepsTheTaskReadyAndStopsTheRetries(t *testing.T) {
	a := readyWithApproval(t, AnswerAllow)
	d, err := a.RaisePublishFailed("pf1", "the branch is not a fast-forward", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != DecisionQuestion || !d.Blocking || d.Cause != CausePublishFailed || d.RunID != "" || d.SHA != "aaa111" {
		t.Errorf("decision %+v", d)
	}
	if got := strings.Join(d.Options, ","); got != "retry,rework,cancel" {
		t.Errorf("options = %s", got)
	}
	if a.task.State != TaskReadyForReview {
		t.Errorf("task = %s, want ready_for_review", a.task.State)
	}
	if _, _, ok := a.OutstandingPublish(); ok {
		t.Error("an open publish_failed ends the outstanding publish")
	}
	if err := a.Answer("pf1", Response{Option: AnswerRetry, By: "human", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.OutstandingPublish(); !ok {
		t.Error("retry makes the publish outstanding again, under the same approval")
	}
	if a.task.State != TaskReadyForReview {
		t.Errorf("task = %s after retry", a.task.State)
	}
}

func TestPublishFailedCancelAndState(t *testing.T) {
	a := readyWithApproval(t, AnswerAllow)
	if _, err := a.RaisePublishFailed("pf1", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.Answer("pf1", Response{Option: AnswerCancel, By: "human", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskCancelled {
		t.Errorf("task = %s", a.task.State)
	}
	wantConflict(t, errOf(newStoppedAggregate(t).RaisePublishFailed("pf1", "x", time.Now())), RuleTaskState)
}

func TestCheckReceiptLine(t *testing.T) {
	r := CheckReceipt{SHA: "a", Command: "make check", Source: "config", Code: 0, Millis: 1500}
	if got, want := r.Line(), "check make check (from config): passed in 1.5s"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
	r.Code = 2
	if !strings.Contains(r.Line(), "exit status 2") || r.Passed() {
		t.Errorf("line = %q", r.Line())
	}
	r.TimedOut = true
	if !strings.Contains(r.Line(), "timed out") {
		t.Errorf("line = %q", r.Line())
	}
}
