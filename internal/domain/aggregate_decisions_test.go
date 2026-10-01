package domain

import (
	"errors"
	"testing"
	"time"
)

var tNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// runningTask returns a running task with one running run in a running
// environment.
func runningTask(t *testing.T) *TaskAggregate {
	t.Helper()
	a, _, _ := newRunningAggregate(t)
	a.TakeEvents()
	return a
}

func askApproval(t *testing.T, a *TaskAggregate, id ID) *Decision {
	t.Helper()
	d, err := a.RaiseDecision(NewDecision{ID: id, RunID: "r1", Kind: DecisionApproval, Blocking: true, Subject: "Bash", Input: "make", Now: tNow})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func eventKinds(evs []Event) []EventKind {
	var out []EventKind
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func TestRaisingABlockingDecisionMovesTheTask(t *testing.T) {
	a := runningTask(t)
	d := askApproval(t, a, "d1")
	if a.Task.State != TaskAwaitingGuidance || d.TaskID != "t1" || d.Status != DecisionOpen {
		t.Errorf("task %s, decision %+v", a.Task.State, d)
	}
	if got := eventKinds(a.TakeEvents()); len(got) != 2 || got[0] != EventDecisionRaised || got[1] != EventTaskState {
		t.Errorf("events = %v, want the raise and the task move in the order they happened", got)
	}
	if len(a.Decisions) != 1 || a.Decisions[0] != d {
		t.Error("the aggregate must hold its decisions")
	}

	// A non-blocking question leaves the task alone.
	b := runningTask(t)
	if _, err := b.RaiseDecision(NewDecision{ID: "q1", RunID: "r1", Kind: DecisionQuestion, Now: tNow}); err != nil {
		t.Fatal(err)
	}
	if b.Task.State != TaskRunning {
		t.Errorf("a non-blocking question moved the task to %s", b.Task.State)
	}
}

func TestRaiseDecisionGuards(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1")
	wantConflict(t, errOnlyD(a.RaiseDecision(NewDecision{ID: "d1", RunID: "r1", Kind: DecisionQuestion, Now: tNow})), RuleDecisionID)
	wantNotFound(t, errOnlyD(a.RaiseDecision(NewDecision{ID: "d2", RunID: "nope", Kind: DecisionQuestion, Now: tNow})))
	wantConflict(t, errOnlyD(a.RaiseDecision(NewDecision{ID: "d3", TaskID: "other", RunID: "r1", Kind: DecisionQuestion, Now: tNow})), RuleDecisionTask)
	// Validation errors still come from Raise.
	if _, err := a.RaiseDecision(NewDecision{ID: "d4", RunID: "r1", Kind: "poll", Now: tNow}); !errors.Is(err, ErrDecisionKind) {
		t.Errorf("an unknown kind = %v", err)
	}

	// A run that is over raises nothing.
	b, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	wantConflict(t, errOnlyD(b.RaiseDecision(NewDecision{ID: "d1", RunID: "r1", Kind: DecisionQuestion, Now: tNow})), RuleRunLive)
	// A refused raise changes nothing.
	if len(b.Decisions) != 0 || b.Task.State != TaskRunning {
		t.Errorf("a refused raise left %d decisions, task %s", len(b.Decisions), b.Task.State)
	}
}

func errOnlyD(_ *Decision, err error) error { return err }

func TestReviewDecisionNeedsAReadyTaskAndItsCandidate(t *testing.T) {
	a := newStoppedAggregate(t)
	spec := NewDecision{ID: "rv1", Kind: DecisionReview, Blocking: true, SHA: "aaa111", Now: tNow}
	wantConflict(t, errOnlyD(a.RaiseDecision(spec)), RuleTaskState) // still running
	if err := a.MarkReady(false); err != nil {
		t.Fatal(err)
	}
	spec.SHA = "zzz999"
	wantNotFound(t, errOnlyD(a.RaiseDecision(spec)))
	spec.SHA = "aaa111"
	if _, err := a.RaiseDecision(spec); err != nil {
		t.Fatal(err)
	}
	if a.Task.State != TaskReadyForReview {
		t.Errorf("a review decision moved the task to %s", a.Task.State)
	}
}

func TestAnsweringReturnsTheTaskToRunning(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1")
	askApproval(t, a, "d2")
	if err := a.Answer("d1", allow(tNow.Add(time.Second), "")); err != nil {
		t.Fatal(err)
	}
	if a.Task.State != TaskAwaitingGuidance {
		t.Errorf("one blocking decision is still open, task is %s", a.Task.State)
	}
	if err := a.Answer("d2", Response{By: "w", Option: AnswerDeny, At: tNow.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if a.Task.State != TaskRunning {
		t.Errorf("task = %s, want running when none is open", a.Task.State)
	}
	wantNotFound(t, a.Answer("nope", allow(tNow, "")))
}

// A late answer expires the decision and still frees the task.
func TestARefusedAnswerStillSettlesTheTask(t *testing.T) {
	a := runningTask(t)
	d := askApproval(t, a, "d1")
	err := a.Answer("d1", allow(d.Deadline, ""))
	if !errors.Is(err, ErrDecisionExpired) {
		t.Fatalf("Answer = %v, want ErrDecisionExpired", err)
	}
	if d.Status != DecisionExpired || a.Task.State != TaskRunning {
		t.Errorf("decision %s, task %s; want expired and running", d.Status, a.Task.State)
	}
}

func TestPauseSupersedesTheRunsOpenDecisions(t *testing.T) {
	a := runningTask(t)
	d1 := askApproval(t, a, "d1")
	q, err := a.RaiseDecision(NewDecision{ID: "q1", RunID: "r1", Kind: DecisionQuestion, Blocking: true, Now: tNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Pause("r1"); err != nil {
		t.Fatal(err)
	}
	if d1.Status != DecisionSuperseded || q.Status != DecisionSuperseded {
		t.Errorf("statuses %s, %s; want both superseded", d1.Status, q.Status)
	}
	if a.Task.State != TaskRunning {
		t.Errorf("a human pause leaves the task running, got %s", a.Task.State)
	}
	if err := a.Answer("d1", allow(tNow, "")); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("an answer to a superseded decision = %v, want ErrDecisionClosed", err)
	}
}

func TestSuspendRunForAuthRaisesAFixedQuestion(t *testing.T) {
	a := runningTask(t)
	old := askApproval(t, a, "d1")
	d, err := a.SuspendRun("r1", CauseAuthExpired, time.Time{}, "auth1", tNow)
	if err != nil {
		t.Fatal(err)
	}
	if a.Runs[0].State != RunPaused || a.Task.State != TaskAwaitingGuidance || old.Status != DecisionSuperseded {
		t.Errorf("run %s, task %s, approval %s", a.Runs[0].State, a.Task.State, old.Status)
	}
	if d.Kind != DecisionQuestion || !d.Blocking || d.Cause != CauseAuthExpired || !d.Deadline.IsZero() || d.RunID != "r1" {
		t.Errorf("decision = %+v", d)
	}
	if got := d.Options; len(got) != 2 || got[0] != AnswerResume || got[1] != AnswerCancel {
		t.Errorf("options = %v, want resume and cancel", got)
	}
}

func TestSuspendRunForQuotaOffersResetOnlyWhenKnown(t *testing.T) {
	reset := tNow.Add(2 * time.Hour)
	a := runningTask(t)
	d, err := a.SuspendRun("r1", CauseQuotaExhausted, reset, "quota1", tNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Options; len(got) != 3 || got[0] != AnswerResume || got[1] != AnswerResumeAtReset || got[2] != AnswerCancel || !d.ResumeAt.Equal(reset) {
		t.Errorf("options %v, resume at %v", got, d.ResumeAt)
	}
	b := runningTask(t)
	d, _ = b.SuspendRun("r1", CauseQuotaExhausted, time.Time{}, "quota1", tNow)
	if len(d.Options) != 2 {
		t.Errorf("without a reset time the options are %v, want resume and cancel", d.Options)
	}
	wantInvalid(t, errOnlyD(runningTask(t).SuspendRun("r1", "boredom", time.Time{}, "x", tNow)))
	// Only a running run is suspended.
	c := runningTask(t)
	c.Runs[0].State = RunStarting
	wantConflict(t, errOnlyD(c.SuspendRun("r1", CauseAuthExpired, time.Time{}, "x", tNow)), RuleTransition)
	if len(c.Decisions) != 0 || c.Runs[0].State != RunStarting {
		t.Error("a refused suspend changed something")
	}
}

func wantInvalid(t *testing.T, err error) {
	t.Helper()
	var ie *InvalidError
	if !errors.As(err, &ie) {
		t.Errorf("error = %v, want an InvalidError", err)
	}
}

func TestResumeOfASuspendedRun(t *testing.T) {
	a := runningTask(t)
	d, _ := a.SuspendRun("r1", CauseAuthExpired, time.Time{}, "auth1", tNow)
	// The login question is open: resuming by hand is refused.
	wantConflict(t, a.Resume("r1"), RuleDecisionOpen)

	if err := a.Answer(d.ID, Response{By: "w", Option: AnswerResume, At: tNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if a.Runs[0].State != RunStarting || a.Task.State != TaskRunning {
		t.Errorf("after the answer: run %s, task %s; want starting and running", a.Runs[0].State, a.Task.State)
	}
}

func TestAnsweringResumeNeedsTheEnvironment(t *testing.T) {
	a := runningTask(t)
	d, _ := a.SuspendRun("r1", CauseAuthExpired, time.Time{}, "auth1", tNow)
	a.Envs["e1"].State = EnvStopped
	wantConflict(t, a.Answer(d.ID, Response{By: "w", Option: AnswerResume, At: tNow}), RuleEnvRunning)
	if d.Status != DecisionOpen || a.Runs[0].State != RunPaused {
		t.Errorf("a refused resume answer changed the decision to %s and the run to %s", d.Status, a.Runs[0].State)
	}
}

func TestAnsweringCancelCancelsTheTask(t *testing.T) {
	a := runningTask(t)
	d, _ := a.SuspendRun("r1", CauseQuotaExhausted, tNow.Add(time.Hour), "q1", tNow)
	if err := a.Answer(d.ID, Response{By: "w", Option: AnswerCancel, At: tNow}); err != nil {
		t.Fatal(err)
	}
	if a.Task.State != TaskCancelled || a.Runs[0].State != RunStopped {
		t.Errorf("task %s, run %s; want cancelled and stopped", a.Task.State, a.Runs[0].State)
	}
}

func TestResumeAtReset(t *testing.T) {
	reset := tNow.Add(2 * time.Hour)
	a := runningTask(t)
	d, _ := a.SuspendRun("r1", CauseQuotaExhausted, reset, "q1", tNow)
	if err := a.Answer(d.ID, Response{By: "w", Option: AnswerResumeAtReset, At: tNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if a.Runs[0].State != RunPaused {
		t.Fatalf("run = %s, want it to wait", a.Runs[0].State)
	}
	if got := a.DueResumes(reset.Add(-time.Second)); len(got) != 0 {
		t.Errorf("due before the reset: %v", got)
	}
	got := a.DueResumes(reset)
	if len(got) != 1 || got[0] != "r1" {
		t.Fatalf("due at the reset = %v, want r1", got)
	}
	if err := a.Resume("r1"); err != nil {
		t.Fatal(err)
	}
	if got := a.DueResumes(reset.Add(time.Hour)); len(got) != 0 {
		t.Errorf("a resumed run is not due again: %v", got)
	}
}

func TestCancelStopsTheRunAndSupersedesEverything(t *testing.T) {
	a := newStoppedAggregate(t)
	if err := a.MarkReady(false); err != nil {
		t.Fatal(err)
	}
	rv, err := a.RaiseDecision(NewDecision{ID: "rv1", Kind: DecisionReview, Blocking: true, SHA: "aaa111", Now: tNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Cancel(); err != nil {
		t.Fatal(err)
	}
	if a.Task.State != TaskCancelled || rv.Status != DecisionSuperseded {
		t.Errorf("task %s, review %s", a.Task.State, rv.Status)
	}
	wantConflict(t, a.Cancel(), RuleTransition) // a cancelled task cannot be cancelled again

	b := runningTask(t)
	d := askApproval(t, b, "d1")
	if err := b.Cancel(); err != nil {
		t.Fatal(err)
	}
	if b.Runs[0].State != RunStopped || d.Status != DecisionSuperseded || b.LiveRun() != nil {
		t.Errorf("run %s, approval %s", b.Runs[0].State, d.Status)
	}
}

func TestInterruptKeepsLoginQuestionsAndReviews(t *testing.T) {
	a := runningTask(t)
	appr := askApproval(t, a, "d1")
	if err := a.Interrupt("r1"); err != nil {
		t.Fatal(err)
	}
	if a.Runs[0].State != RunInterrupted || appr.Status != DecisionSuperseded || a.Task.State != TaskRunning {
		t.Errorf("run %s, approval %s, task %s", a.Runs[0].State, appr.Status, a.Task.State)
	}

	b := runningTask(t)
	login, _ := b.SuspendRun("r1", CauseAuthExpired, time.Time{}, "auth1", tNow)
	if err := b.Interrupt("r1"); err != nil {
		t.Fatal(err)
	}
	if login.Status != DecisionOpen || b.Task.State != TaskAwaitingGuidance {
		t.Errorf("a login question must survive a restart: %s, task %s", login.Status, b.Task.State)
	}
	// Only a live run is interrupted.
	c := runningTask(t)
	c.Runs[0].State = RunStopped
	wantConflict(t, c.Interrupt("r1"), RuleTransition)
	wantNotFound(t, c.Interrupt("nope"))
}

func TestObserveEnvInterruptsTheRunsInIt(t *testing.T) {
	a := runningTask(t)
	d := askApproval(t, a, "d1")
	a.TakeEvents()
	if err := a.ObserveEnv("e1", EnvStopped); err != nil {
		t.Fatal(err)
	}
	if a.Envs["e1"].State != EnvStopped || a.Runs[0].State != RunInterrupted || d.Status != DecisionSuperseded {
		t.Errorf("env %s, run %s, approval %s", a.Envs["e1"].State, a.Runs[0].State, d.Status)
	}
	got := eventKinds(a.TakeEvents())
	hasEnv := false
	for _, k := range got {
		hasEnv = hasEnv || k == EventEnvState
	}
	if !hasEnv {
		t.Errorf("the observation must be recorded: %v", got)
	}

	// The environment comes back.
	if err := a.ObserveEnv("e1", EnvRunning); err != nil {
		t.Fatal(err)
	}
	if a.Envs["e1"].State != EnvRunning {
		t.Errorf("env = %s", a.Envs["e1"].State)
	}
	// An observation that changes nothing records nothing.
	a.TakeEvents()
	if err := a.ObserveEnv("e1", EnvRunning); err != nil || len(a.PendingEvents()) != 0 {
		t.Errorf("same state: %v, %d events", err, len(a.PendingEvents()))
	}
	wantNotFound(t, a.ObserveEnv("nope", EnvStopped))
	wantConflict(t, a.ObserveEnv("e1", EnvProvisioning), RuleTransition)
}

func TestObserveEnvGone(t *testing.T) {
	a := runningTask(t)
	if err := a.ObserveEnv("e1", EnvDeleted); err != nil {
		t.Fatal(err)
	}
	if a.Envs["e1"].State != EnvDeleted || a.Runs[0].State != RunInterrupted {
		t.Errorf("env %s, run %s", a.Envs["e1"].State, a.Runs[0].State)
	}
}

func TestRecordSession(t *testing.T) {
	a := runningTask(t)
	if err := a.RecordSession("r1", "s-1"); err != nil {
		t.Fatal(err)
	}
	if a.Runs[0].SessionID != "s-1" {
		t.Errorf("session = %q", a.Runs[0].SessionID)
	}
	a.TakeEvents()
	if err := a.RecordSession("r1", "s-1"); err != nil || len(a.PendingEvents()) != 0 {
		t.Errorf("the same ID again: %v, %d events", err, len(a.PendingEvents()))
	}
	wantConflict(t, a.RecordSession("r1", "s-2"), RuleSessionID)
	wantConflict(t, a.RecordSession("r1", ""), RuleSessionID)
	wantNotFound(t, a.RecordSession("nope", "s"))
}

func TestMarkRunningAndFailRun(t *testing.T) {
	a := runningTask(t)
	a.Runs[0].State = RunStarting
	if err := a.MarkRunning("r1"); err != nil || a.Runs[0].State != RunRunning {
		t.Fatalf("MarkRunning: %v, run %s", err, a.Runs[0].State)
	}
	d, err := a.FailRun("r1", "fail1", tNow)
	if err != nil {
		t.Fatal(err)
	}
	if a.Runs[0].State != RunFailed || a.Task.State != TaskAwaitingGuidance || d.Cause != CauseRunFailed ||
		len(d.Options) != 2 || d.Options[0] != AnswerRetry || d.Options[1] != AnswerCancel {
		t.Errorf("run %s, task %s, decision %+v", a.Runs[0].State, a.Task.State, d)
	}
	// Retry frees the task for a new run.
	if err := a.Answer("fail1", Response{By: "w", Option: AnswerRetry, At: tNow}); err != nil {
		t.Fatal(err)
	}
	if a.Task.State != TaskRunning || a.LiveRun() != nil {
		t.Errorf("task %s, live run %v", a.Task.State, a.LiveRun())
	}
	if err := a.StartRun(&Run{ID: "r2", EnvID: "e1"}); err != nil {
		t.Errorf("a retry run: %v", err)
	}
}
