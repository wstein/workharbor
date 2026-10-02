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
	if _, err := a.RaiseDecision(NewDecision{ID: id, RunID: "r1", Kind: DecisionApproval, Blocking: true, Subject: "Bash", Input: "make", Now: tNow}); err != nil {
		t.Fatal(err)
	}
	return a.mustDecision(id)
}

// mustDecision returns the aggregate's own Decision, so a test sees later changes.
func (a *TaskAggregate) mustDecision(id ID) *Decision {
	d, err := a.decision(id)
	if err != nil {
		panic(err)
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
	if a.task.State != TaskAwaitingGuidance || d.TaskID != "t1" || d.Status != DecisionOpen {
		t.Errorf("task %s, decision %+v", a.task.State, d)
	}
	if got := eventKinds(a.TakeEvents()); len(got) != 2 || got[0] != EventDecisionRaised || got[1] != EventTaskState {
		t.Errorf("events = %v, want the raise and the task move in the order they happened", got)
	}
	if len(a.decisions) != 1 || a.decisions[0] != d {
		t.Error("the aggregate must hold its decisions")
	}

	// A non-blocking question leaves the task alone.
	b := runningTask(t)
	if _, err := b.RaiseDecision(NewDecision{ID: "q1", RunID: "r1", Kind: DecisionQuestion, Now: tNow}); err != nil {
		t.Fatal(err)
	}
	if b.task.State != TaskRunning {
		t.Errorf("a non-blocking question moved the task to %s", b.task.State)
	}
}

func TestRaiseDecisionGuards(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1")
	wantConflict(t, errOnly(a.RaiseDecision(NewDecision{ID: "d1", RunID: "r1", Kind: DecisionQuestion, Now: tNow})), RuleDecisionID)
	wantNotFound(t, errOnly(a.RaiseDecision(NewDecision{ID: "d2", RunID: "nope", Kind: DecisionQuestion, Now: tNow})))
	wantConflict(t, errOnly(a.RaiseDecision(NewDecision{ID: "d3", TaskID: "other", RunID: "r1", Kind: DecisionQuestion, Now: tNow})), RuleDecisionTask)
	// Validation errors still come from Raise.
	if _, err := a.RaiseDecision(NewDecision{ID: "d4", RunID: "r1", Kind: "poll", Now: tNow}); !errors.Is(err, ErrDecisionKind) {
		t.Errorf("an unknown kind = %v", err)
	}

	// A run that is over raises nothing.
	b, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	wantConflict(t, errOnly(b.RaiseDecision(NewDecision{ID: "d1", RunID: "r1", Kind: DecisionQuestion, Now: tNow})), RuleRunLive)
	// A refused raise changes nothing.
	if len(b.decisions) != 0 || b.task.State != TaskRunning {
		t.Errorf("a refused raise left %d decisions, task %s", len(b.decisions), b.task.State)
	}
}

func TestReviewDecisionNeedsAReadyTaskAndItsCandidate(t *testing.T) {
	a := newStoppedAggregate(t)
	spec := NewDecision{ID: "rv1", Kind: DecisionReview, Blocking: true, SHA: "aaa111", Now: tNow}
	wantConflict(t, errOnly(a.RaiseDecision(spec)), RuleTaskState) // still running
	if err := a.MarkReady(false); err != nil {
		t.Fatal(err)
	}
	spec.SHA = "zzz999"
	wantNotFound(t, errOnly(a.RaiseDecision(spec)))
	spec.SHA = "aaa111"
	if _, err := a.RaiseDecision(spec); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskReadyForReview {
		t.Errorf("a review decision moved the task to %s", a.task.State)
	}
}

func TestAnsweringReturnsTheTaskToRunning(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1")
	askApproval(t, a, "d2")
	if err := a.Answer("d1", allow(tNow.Add(time.Second), "")); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskAwaitingGuidance {
		t.Errorf("one blocking decision is still open, task is %s", a.task.State)
	}
	if err := a.Answer("d2", Response{By: "w", Option: AnswerDeny, At: tNow.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskRunning {
		t.Errorf("task = %s, want running when none is open", a.task.State)
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
	if d.Status != DecisionExpired || a.task.State != TaskRunning {
		t.Errorf("decision %s, task %s; want expired and running", d.Status, a.task.State)
	}
}

func TestPauseSupersedesTheRunsOpenDecisions(t *testing.T) {
	a := runningTask(t)
	d1 := askApproval(t, a, "d1")
	_, err := a.RaiseDecision(NewDecision{ID: "q1", RunID: "r1", Kind: DecisionQuestion, Blocking: true, Now: tNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Pause("r1"); err != nil {
		t.Fatal(err)
	}
	if q := a.mustDecision("q1"); d1.Status != DecisionSuperseded || q.Status != DecisionSuperseded {
		t.Errorf("statuses %s, %s; want both superseded", d1.Status, q.Status)
	}
	if a.task.State != TaskRunning {
		t.Errorf("a human pause leaves the task running, got %s", a.task.State)
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
	if a.runs[0].State != RunPaused || a.task.State != TaskAwaitingGuidance || old.Status != DecisionSuperseded {
		t.Errorf("run %s, task %s, approval %s", a.runs[0].State, a.task.State, old.Status)
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
	wantInvalid(t, errOnly(runningTask(t).SuspendRun("r1", "boredom", time.Time{}, "x", tNow)))
	// Only a running run is suspended.
	c := runningTask(t)
	c.runs[0].State = RunStarting
	wantConflict(t, errOnly(c.SuspendRun("r1", CauseAuthExpired, time.Time{}, "x", tNow)), RuleTransition)
	if len(c.decisions) != 0 || c.runs[0].State != RunStarting {
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
	if a.runs[0].State != RunStarting || a.task.State != TaskRunning {
		t.Errorf("after the answer: run %s, task %s; want starting and running", a.runs[0].State, a.task.State)
	}
}

func TestAnsweringResumeNeedsTheEnvironment(t *testing.T) {
	a := runningTask(t)
	d, _ := a.SuspendRun("r1", CauseAuthExpired, time.Time{}, "auth1", tNow)
	a.envs["e1"].State = EnvStopped
	wantConflict(t, a.Answer(d.ID, Response{By: "w", Option: AnswerResume, At: tNow}), RuleEnvRunning)
	if d := a.mustDecision("auth1"); d.Status != DecisionOpen || a.runs[0].State != RunPaused {
		t.Errorf("a refused resume answer changed the decision to %s and the run to %s", d.Status, a.runs[0].State)
	}
}

func TestAnsweringCancelCancelsTheTask(t *testing.T) {
	a := runningTask(t)
	d, _ := a.SuspendRun("r1", CauseQuotaExhausted, tNow.Add(time.Hour), "q1", tNow)
	if err := a.Answer(d.ID, Response{By: "w", Option: AnswerCancel, At: tNow}); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskCancelled || a.runs[0].State != RunStopped {
		t.Errorf("task %s, run %s; want cancelled and stopped", a.task.State, a.runs[0].State)
	}
}

func TestResumeAtReset(t *testing.T) {
	reset := tNow.Add(2 * time.Hour)
	a := runningTask(t)
	d, _ := a.SuspendRun("r1", CauseQuotaExhausted, reset, "q1", tNow)
	if err := a.Answer(d.ID, Response{By: "w", Option: AnswerResumeAtReset, At: tNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if a.runs[0].State != RunPaused {
		t.Fatalf("run = %s, want it to wait", a.runs[0].State)
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
	_, err := a.RaiseDecision(NewDecision{ID: "rv1", Kind: DecisionReview, Blocking: true, SHA: "aaa111", Now: tNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Cancel(); err != nil {
		t.Fatal(err)
	}
	if rv := a.mustDecision("rv1"); a.task.State != TaskCancelled || rv.Status != DecisionSuperseded {
		t.Errorf("task %s, review %s", a.task.State, rv.Status)
	}
	wantConflict(t, a.Cancel(), RuleTransition) // a cancelled task cannot be cancelled again

	b := runningTask(t)
	d := askApproval(t, b, "d1")
	if err := b.Cancel(); err != nil {
		t.Fatal(err)
	}
	if b.runs[0].State != RunStopped || d.Status != DecisionSuperseded || b.liveRun() != nil {
		t.Errorf("run %s, approval %s", b.runs[0].State, d.Status)
	}
}

func TestInterruptKeepsLoginQuestionsAndReviews(t *testing.T) {
	a := runningTask(t)
	appr := askApproval(t, a, "d1")
	if err := a.Interrupt("r1"); err != nil {
		t.Fatal(err)
	}
	if a.runs[0].State != RunInterrupted || appr.Status != DecisionSuperseded || a.task.State != TaskRunning {
		t.Errorf("run %s, approval %s, task %s", a.runs[0].State, appr.Status, a.task.State)
	}

	b := runningTask(t)
	_, _ = b.SuspendRun("r1", CauseAuthExpired, time.Time{}, "auth1", tNow)
	if err := b.Interrupt("r1"); err != nil {
		t.Fatal(err)
	}
	if login := b.mustDecision("auth1"); login.Status != DecisionOpen || b.task.State != TaskAwaitingGuidance {
		t.Errorf("a login question must survive a restart: %s, task %s", login.Status, b.task.State)
	}
	// Only a live run is interrupted.
	c := runningTask(t)
	c.runs[0].State = RunStopped
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
	if a.envs["e1"].State != EnvStopped || a.runs[0].State != RunInterrupted || d.Status != DecisionSuperseded {
		t.Errorf("env %s, run %s, approval %s", a.envs["e1"].State, a.runs[0].State, d.Status)
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
	if a.envs["e1"].State != EnvRunning {
		t.Errorf("env = %s", a.envs["e1"].State)
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
	if a.envs["e1"].State != EnvDeleted || a.runs[0].State != RunInterrupted {
		t.Errorf("env %s, run %s", a.envs["e1"].State, a.runs[0].State)
	}
}

func TestRecordSession(t *testing.T) {
	a := runningTask(t)
	if err := a.RecordSession("r1", "s-1"); err != nil {
		t.Fatal(err)
	}
	if a.runs[0].SessionID != "s-1" {
		t.Errorf("session = %q", a.runs[0].SessionID)
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
	a.runs[0].State = RunStarting
	if err := a.MarkRunning("r1"); err != nil || a.runs[0].State != RunRunning {
		t.Fatalf("MarkRunning: %v, run %s", err, a.runs[0].State)
	}
	d, err := a.FailRun("r1", "fail1", tNow)
	if err != nil {
		t.Fatal(err)
	}
	if a.runs[0].State != RunFailed || a.task.State != TaskAwaitingGuidance || d.Cause != CauseRunFailed ||
		len(d.Options) != 2 || d.Options[0] != AnswerRetry || d.Options[1] != AnswerCancel {
		t.Errorf("run %s, task %s, decision %+v", a.runs[0].State, a.task.State, d)
	}
	// Retry frees the task for a new run.
	if err := a.Answer("fail1", Response{By: "w", Option: AnswerRetry, At: tNow}); err != nil {
		t.Fatal(err)
	}
	if a.task.State != TaskRunning || a.liveRun() != nil {
		t.Errorf("task %s, live run %v", a.task.State, a.liveRun())
	}
	if err := a.StartRun(Run{ID: "r2", EnvID: "e1"}); err != nil {
		t.Errorf("a retry run: %v", err)
	}
}

func TestExpireDecisionsFreesTheTask(t *testing.T) {
	a := runningTask(t)
	d := askApproval(t, a, "d1")
	if got := a.ExpireDecisions(d.Deadline.Add(-time.Second)); len(got) != 0 || a.task.State != TaskAwaitingGuidance {
		t.Errorf("before the deadline: %v, task %s", got, a.task.State)
	}
	got := a.ExpireDecisions(d.Deadline)
	if len(got) != 1 || got[0] != "d1" || d.Status != DecisionExpired || a.task.State != TaskRunning || d.Allows("") {
		t.Errorf("expired %v, status %s, task %s", got, d.Status, a.task.State)
	}
}

func TestReraiseDecisionAfterAResume(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1")
	if err := a.Interrupt("r1"); err != nil {
		t.Fatal(err)
	}
	// The run must be live again before it asks again.
	wantConflict(t, errOnly(a.ReraiseDecision("d1", "d1b", tNow)), RuleRunLive)
	if err := a.Resume("r1"); err != nil {
		t.Fatal(err)
	}
	n, err := a.ReraiseDecision("d1", "d1b", tNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != "d1b" || a.mustDecision("d1").SupersededBy != "d1b" || a.task.State != TaskAwaitingGuidance {
		t.Errorf("new %+v, old %+v, task %s", n, a.mustDecision("d1"), a.task.State)
	}
	wantConflict(t, errOnly(a.ReraiseDecision("d1", "d1c", tNow)), RuleAlreadyRaised)
	wantNotFound(t, errOnly(a.ReraiseDecision("nope", "x", tNow)))
}

func TestSnapshotAndRestore(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1")
	if err := a.RecordSession("r1", "s-1"); err != nil {
		t.Fatal(err)
	}
	snap := a.Snapshot()
	if len(snap.Runs) != 1 || len(snap.Envs) != 1 || len(snap.Decisions) != 1 || !snap.Decisions[0].Changed || snap.Runs[0].SessionID != "s-1" {
		t.Fatalf("snapshot = %+v", snap)
	}
	// A snapshot is a copy.
	snap.Runs[0].State = RunFailed
	snap.Task.State = TaskFailed
	if a.runs[0].State == RunFailed || a.task.State == TaskFailed {
		t.Error("changing a snapshot changed the aggregate")
	}

	good := a.Snapshot()
	good.Decisions[0].Changed = false
	b, err := Restore(good)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.PendingEvents()) != 0 || b.Task().State != TaskAwaitingGuidance || len(b.Decisions()) != 1 {
		t.Errorf("restored: %d events, task %s", len(b.PendingEvents()), b.Task().State)
	}
	// The restored aggregate's guards work.
	wantConflict(t, b.StartRun(Run{ID: "r2", EnvID: "e1"}), RuleTaskState)

	b.MarkSaved(7, map[ID]int64{"d1": 3})
	if b.Task().Version != 7 || b.Decisions()[0].Version != 3 {
		t.Errorf("versions %d, %d", b.Task().Version, b.Decisions()[0].Version)
	}
}

func TestRestoreRefusesAContradiction(t *testing.T) {
	base := func() Snapshot {
		a := runningTask(t)
		askApproval(t, a, "d1")
		return a.Snapshot()
	}
	tests := map[string]func(*Snapshot){
		"a run in an unknown environment": func(s *Snapshot) { s.Runs[0].EnvID = "nope" },
		"a repeated run":                  func(s *Snapshot) { s.Runs = append(s.Runs, s.Runs[0]) },
		"two live runs":                   func(s *Snapshot) { s.Runs = append(s.Runs, Run{ID: "r2", EnvID: "e1", State: RunRunning}) },
		"a candidate for an unknown run":  func(s *Snapshot) { s.Candidates = []ReviewCandidate{{RunID: "nope", SHA: "a"}} },
		"a repeated candidate":            func(s *Snapshot) { s.Candidates = []ReviewCandidate{{RunID: "r1", SHA: "a"}, {RunID: "r1", SHA: "a"}} },
		"a decision for another task":     func(s *Snapshot) { s.Decisions[0].Decision.TaskID = "other" },
		"a decision for an unknown run":   func(s *Snapshot) { s.Decisions[0].Decision.RunID = "nope" },
		"a repeated decision":             func(s *Snapshot) { s.Decisions = append(s.Decisions, s.Decisions[0]) },
		"an empty environment ID":         func(s *Snapshot) { s.Envs = append(s.Envs, Environment{}) },
	}
	for name, mod := range tests {
		snap := base()
		mod(&snap)
		if _, err := Restore(snap); err == nil {
			t.Errorf("%s: Restore accepted it", name)
		}
	}
}

func TestStopRunEndsTheRun(t *testing.T) {
	a := runningTask(t)
	d := askApproval(t, a, "d1")
	if err := a.StopRun("r1"); err != nil {
		t.Fatal(err)
	}
	if a.runs[0].State != RunStopped || d.Status != DecisionSuperseded || a.task.State != TaskRunning {
		t.Errorf("run %s, approval %s, task %s", a.runs[0].State, d.Status, a.task.State)
	}
	wantConflict(t, a.StopRun("r1"), RuleTransition) // a stopped run stays stopped
	wantNotFound(t, a.StopRun("nope"))
}

func TestAddingAnEnvironmentIsRecordedButRestoringIsNot(t *testing.T) {
	a := NewTaskAggregate(Task{ID: "t1", State: TaskRunning})
	a.AddEnvironment(Environment{ID: "e1", Backend: "apple", State: EnvRunning})
	if got := kinds(a.PendingEvents()); len(got) != 1 || got[0] != EventEnvAdded {
		t.Errorf("events = %v, want env.added", got)
	}
	snap := a.Snapshot()
	b, err := Restore(snap)
	if err != nil || len(b.PendingEvents()) != 0 {
		t.Errorf("a restored aggregate has %d events (%v)", len(b.PendingEvents()), err)
	}
}

func TestRestoreRefusesAPendingChange(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1") // a changed, unsaved decision
	if _, err := Restore(a.Snapshot()); err == nil {
		t.Error("Restore accepted a snapshot with a pending change")
	}
}

func TestResumeBlockedAndWaitsForReset(t *testing.T) {
	reset := tNow.Add(time.Hour)
	a := runningTask(t)
	d, err := a.SuspendRun("r1", CauseQuotaExhausted, reset, "q1", tNow)
	if err != nil {
		t.Fatal(err)
	}
	wantConflict(t, a.ResumeBlocked("r1"), RuleDecisionOpen) // the question is open
	if a.WaitsForReset("r1", tNow) {
		t.Error("an open question is not a choice to wait")
	}
	must(t, a.Answer(d.ID, Response{By: "w", Option: AnswerResumeAtReset, At: tNow}))
	must(t, a.ResumeBlocked("r1")) // answered: nothing blocks, but it waits
	if !a.WaitsForReset("r1", tNow) || a.WaitsForReset("r1", reset) {
		t.Error("the run waits until the reset time and not after")
	}
	// An interrupted run keeps waiting.
	must(t, a.Interrupt("r1"))
	if !a.WaitsForReset("r1", tNow.Add(time.Minute)) {
		t.Error("an interruption must not end the wait")
	}
	// A run in a state that cannot resume is blocked too.
	b := runningTask(t)
	wantConflict(t, b.ResumeBlocked("r1"), RuleTransition)
	wantNotFound(t, b.ResumeBlocked("nope"))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLaunchFailuresAreCountedAndUsedUp(t *testing.T) {
	a := runningTask(t)
	must(t, a.Interrupt("r1"))
	for attempt := 1; attempt <= 3; attempt++ {
		must(t, a.Resume("r1"))
		exhausted, err := a.RecordLaunchFailure("r1", 3)
		if err != nil {
			t.Fatal(err)
		}
		if exhausted != (attempt == 3) || a.runs[0].ResumeAttempts != attempt || a.runs[0].State != RunInterrupted {
			t.Fatalf("attempt %d: exhausted %v, attempts %d, run %s", attempt, exhausted, a.runs[0].ResumeAttempts, a.runs[0].State)
		}
	}
	// An exhausted run can be failed, which opens the retry or cancel question.
	if _, err := a.FailRun("r1", "fail1", tNow); err != nil {
		t.Fatal(err)
	}
	// A run that starts running again forgets the failures.
	b := runningTask(t)
	must(t, b.Interrupt("r1"))
	must(t, b.Resume("r1"))
	_, _ = b.RecordLaunchFailure("r1", 3)
	must(t, b.Resume("r1"))
	must(t, b.MarkRunning("r1"))
	if b.runs[0].ResumeAttempts != 0 {
		t.Errorf("attempts = %d after the run ran again", b.runs[0].ResumeAttempts)
	}
	_, err := b.RecordLaunchFailure("r1", 3)
	wantConflict(t, err, RuleTransition) // it is running now, not starting
}

func TestSupersededOfListsWhatTheAgentAsked(t *testing.T) {
	a := runningTask(t)
	askApproval(t, a, "d1")
	must(t, a.Interrupt("r1"))
	got := a.SupersededOf("r1")
	if len(got) != 1 || got[0].Subject != "Bash" || got[0].Input != "make" {
		t.Errorf("superseded = %+v", got)
	}
	must(t, a.Resume("r1"))
	if _, err := a.ReraiseDecision("d1", "d1b", tNow); err != nil {
		t.Fatal(err)
	}
	if got := a.SupersededOf("r1"); len(got) != 0 {
		t.Errorf("a decision raised again is not listed: %+v", got)
	}
}

// An input the caller already cut makes a truncated Decision, though what is
// left fits: the human must see that an allow covers more than is shown.
func TestADecisionKeepsTheCallersTruncation(t *testing.T) {
	a := runningTask(t)
	d, err := a.RaiseDecision(NewDecision{ID: "d1", RunID: "r1", Kind: DecisionApproval, Blocking: true, Subject: "Bash", Input: "make", InputTruncated: true, Now: tNow})
	if err != nil {
		t.Fatal(err)
	}
	if !d.InputTruncated || d.Input != "make" {
		t.Errorf("Decision input %q truncated %v, want %q true", d.Input, d.InputTruncated, "make")
	}
}
