package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/exitcode"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// newAggregate builds a task with an environment and a started run.
func newAggregate(t *testing.T, id domain.ID) *domain.TaskAggregate {
	t.Helper()
	a := domain.NewTaskAggregate(domain.Task{ID: id, Repo: "wstein/workharbor", Issue: "#15", State: domain.TaskRunning, CreatedAt: t0})
	a.AddEnvironment(domain.Environment{ID: domain.ID("e-" + string(id)), Backend: "apple", State: domain.EnvRunning})
	if err := a.StartRun(domain.Run{ID: domain.ID("r-" + string(id)), WorkspaceID: "w1", EnvID: domain.ID("e-" + string(id))}); err != nil {
		t.Fatal(err)
	}
	return a
}

func eventKinds(events []domain.Event) []domain.EventKind {
	var out []domain.EventKind
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

// running moves the aggregate's started run to running.
func running(t *testing.T, a *domain.TaskAggregate) {
	t.Helper()
	if err := a.MarkRunning(a.Runs()[0].ID); err != nil {
		t.Fatal(err)
	}
}

func approve(t *testing.T, a *domain.TaskAggregate, id domain.ID) {
	t.Helper()
	if _, err := a.RaiseDecision(domain.NewDecision{
		ID: id, RunID: a.Runs()[0].ID, Kind: domain.DecisionApproval, Blocking: true,
		Subject: "Bash", Input: "make deploy", Now: t0,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSaveAndLoadATaskRoundTrips(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	running(t, a)
	if err := a.Pause("r-t1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Resume("r-t1"); err != nil {
		t.Fatal(err)
	}
	running(t, a)
	if _, err := a.PinRevision("r-t1", "agent/topic", "aaa111"); err != nil {
		t.Fatal(err)
	}
	if err := a.StopRun("r-t1"); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordCI("aaa111", domain.CIPassed); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordPR("aaa111", "https://github.com/wstein/workharbor/pull/22"); err != nil {
		t.Fatal(err)
	}

	events, err := s.SaveTask(bg, a)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.EventKind{
		domain.EventEnvAdded, domain.EventRunStarted, domain.EventRunState, domain.EventRunState, domain.EventRunState, domain.EventRunState,
		domain.EventRevisionPinned, domain.EventRunState, domain.EventCIRecorded, domain.EventPRRecorded,
	}
	if !reflect.DeepEqual(eventKinds(events), want) {
		t.Errorf("saved events = %v, want %v", eventKinds(events), want)
	}
	if a.Task().Version != 1 || len(a.PendingEvents()) != 0 {
		t.Errorf("after the save: version %d, %d pending events; want 1 and none", a.Task().Version, len(a.PendingEvents()))
	}

	got, err := s.LoadTask(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Snapshot(), a.Snapshot()) {
		t.Errorf("the loaded aggregate differs:\n got %+v\nwant %+v", got.Snapshot(), a.Snapshot())
	}
	log, _ := s.EventsSince(bg, "t1", 0, 0)
	if !reflect.DeepEqual(eventKinds(log), want) || log[0].Seq != 1 {
		t.Errorf("the log = %v", eventKinds(log))
	}
}

func TestSavingAgainBumpsTheVersionAndAppendsOnlyNewEvents(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	running(t, a)
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	if err := a.Pause("r-t1"); err != nil {
		t.Fatal(err)
	}
	events, err := s.SaveTask(bg, a)
	if err != nil {
		t.Fatal(err)
	}
	if a.Task().Version != 2 || len(events) != 1 || events[0].Kind != domain.EventRunState {
		t.Errorf("version %d, events %v; want 2 and one run.state", a.Task().Version, eventKinds(events))
	}
	// A save with nothing new changes the version but adds no event.
	if events, err = s.SaveTask(bg, a); err != nil || len(events) != 0 || a.Task().Version != 3 {
		t.Errorf("an unchanged save: %d events, version %d, %v", len(events), a.Task().Version, err)
	}
	loaded, _ := s.LoadTask(bg, "t1")
	if loaded.Runs()[0].State != domain.RunPaused || loaded.Task().Version != 3 {
		t.Errorf("loaded run %s at version %d", loaded.Runs()[0].State, loaded.Task().Version)
	}
}

// Two writers load the same task: the second to save loses and writes nothing.
func TestCompareAndSwapOnATask(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	running(t, a)
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	first, _ := s.LoadTask(bg, "t1")
	second, _ := s.LoadTask(bg, "t1")

	if err := first.Pause("r-t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, first); err != nil {
		t.Fatal(err)
	}

	if _, err := second.PinRevision("r-t1", "agent/topic", "bbb222"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.EventsSince(bg, "", 0, 0)
	_, err := s.SaveTask(bg, second)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("the second save = %v, want ErrStale", err)
	}
	if got := exitcode.From(err); got != exitcode.Conflict {
		t.Errorf("exit code = %d, want Conflict (%d)", got, exitcode.Conflict)
	}

	after, _ := s.EventsSince(bg, "", 0, 0)
	if len(after) != len(before) {
		t.Errorf("the lost save wrote %d events", len(after)-len(before))
	}
	stored, _ := s.LoadTask(bg, "t1")
	if stored.Runs()[0].State != domain.RunPaused || len(stored.Candidates()) != 0 {
		t.Errorf("the winner's state was overwritten: run %s, %d candidates", stored.Runs()[0].State, len(stored.Candidates()))
	}
	if second.Task().Version != 1 || len(second.PendingEvents()) == 0 {
		t.Errorf("a lost save must leave the aggregate to retry: version %d, %d pending events", second.Task().Version, len(second.PendingEvents()))
	}
}

func TestTwoNewTasksWithTheSameIDDoNotBothWin(t *testing.T) {
	s := openTemp(t)
	if _, err := s.SaveTask(bg, newAggregate(t, "t1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, newAggregate(t, "t1")); !errors.Is(err, ErrStale) {
		t.Errorf("a second insert of the same task = %v, want ErrStale", err)
	}
}

// restoredAt returns the aggregate as if it had been loaded at a version.
func restoredAt(t *testing.T, a *domain.TaskAggregate, version int64) *domain.TaskAggregate {
	t.Helper()
	snap := a.Snapshot()
	snap.Task.Version = version
	r, err := domain.Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestUnknownTasksAreNotFound(t *testing.T) {
	s := openTemp(t)
	_, err := s.LoadTask(bg, "nope")
	if !errors.Is(err, domain.ErrNotFound) || exitcode.From(err) != exitcode.NotFound {
		t.Errorf("LoadTask = %v (exit %d), want not-found", err, exitcode.From(err))
	}
	// It was loaded from somewhere that no longer has it.
	ghost := restoredAt(t, newAggregate(t, "ghost"), 5)
	if _, err := s.SaveTask(bg, ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("saving a loaded task that is gone = %v, want not-found", err)
	}
}

// The state and its audit events are one unit: nothing is left behind when
// the transaction does not commit.
func TestStateAndEventsAreOneTransaction(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	boom := errors.New("boom")
	err := s.Update(context.Background(), func(tx *Tx) error {
		if _, err := tx.SaveTask(bg, a); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v", err)
	}
	if _, err := s.LoadTask(bg, "t1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the task survived a rolled-back transaction: %v", err)
	}
	if events, _ := s.EventsSince(bg, "", 0, 0); len(events) != 0 {
		t.Errorf("%d events survived a rolled-back transaction", len(events))
	}
	if a.Task().Version != 0 || len(a.PendingEvents()) != 2 {
		t.Errorf("a rolled-back save must leave the aggregate as it was: version %d, %d pending", a.Task().Version, len(a.PendingEvents()))
	}
}

func TestSaveAndLoadADecisionRoundTrips(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	approve(t, a, "d1")
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	if err := a.Answer("d1", domain.Response{By: "werner", Option: domain.AnswerAllow, Reason: "fine", At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	events, err := s.SaveTask(bg, a)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := a.Decision("d1")
	if len(events) != 2 || events[0].Kind != domain.EventDecisionAnswered || events[1].Kind != domain.EventTaskState || d.Version != 2 {
		t.Errorf("events %v, version %d", eventKinds(events), d.Version)
	}

	got, err := s.LoadDecision(bg, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, d) {
		t.Errorf("the loaded decision differs:\n got %+v\nwant %+v", *got, d)
	}
	if !got.Allows("") || got.AnsweredBy != "werner" || got.AnsweredAt == nil || !got.AnsweredAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("an answered allow must survive the store: %+v", got)
	}
	log, _ := s.EventsSince(bg, "t1", 0, 0)
	kinds := eventKinds(log)
	raised, answered := -1, -1
	for i, k := range kinds {
		switch k {
		case domain.EventDecisionRaised:
			raised = i
		case domain.EventDecisionAnswered:
			answered = i
		}
	}
	if raised < 0 || answered < raised {
		t.Errorf("log = %v, want raised before answered", kinds)
	}
}

// An answer and an expiry race: whoever saves first wins, and the other is told.
func TestAnAnswerAndAnExpiryCannotBothWin(t *testing.T) {
	for _, answerFirst := range []bool{true, false} {
		s := openTemp(t)
		a := newAggregate(t, "t1")
		approve(t, a, "d1")
		if _, err := s.SaveTask(bg, a); err != nil {
			t.Fatal(err)
		}
		answerer, _ := s.LoadTask(bg, "t1")
		expirer, _ := s.LoadTask(bg, "t1")
		if err := answerer.Answer("d1", domain.Response{By: "werner", Option: domain.AnswerAllow, At: t0.Add(time.Minute)}); err != nil {
			t.Fatal(err)
		}
		d, _ := expirer.Decision("d1")
		if got := expirer.ExpireDecisions(d.Deadline); len(got) != 1 {
			t.Fatal("setup: the decision should expire")
		}

		order := []*domain.TaskAggregate{answerer, expirer}
		wantStatus := domain.DecisionAnswered
		if !answerFirst {
			order = []*domain.TaskAggregate{expirer, answerer}
			wantStatus = domain.DecisionExpired
		}
		if _, err := s.SaveTask(bg, order[0]); err != nil {
			t.Fatalf("the first save: %v", err)
		}
		loser := order[1]
		if _, err := s.SaveTask(bg, loser); !errors.Is(err, ErrStale) {
			t.Fatalf("answerFirst=%v: the second save = %v, want ErrStale", answerFirst, err)
		}
		if ld, _ := loser.Decision("d1"); len(loser.PendingEvents()) == 0 || ld.Version != 1 {
			t.Errorf("the loser must keep its change to retry: version %d, %d pending", ld.Version, len(loser.PendingEvents()))
		}
		stored, _ := s.LoadDecision(bg, "d1")
		if stored.Status != wantStatus {
			t.Errorf("answerFirst=%v: stored status %s, want %s", answerFirst, stored.Status, wantStatus)
		}
		if wantStatus == domain.DecisionExpired && stored.Allows("") {
			t.Error("an expired approval must not allow, even though an answer was in flight")
		}
	}
}

func TestOpenDecisionsListsOnlyOpenOnesOfTheTask(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	approve(t, a, "d-open")
	approve(t, a, "d-answered")
	if err := a.Answer("d-answered", domain.Response{By: "w", Option: domain.AnswerDeny, At: t0.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	other := newAggregate(t, "t2")
	approve(t, other, "d-other")
	for _, agg := range []*domain.TaskAggregate{a, other} {
		if _, err := s.SaveTask(bg, agg); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.OpenDecisions(bg, "t1")
	if err != nil || len(got) != 1 || got[0].ID != "d-open" {
		t.Errorf("open decisions of t1 = %+v, %v", got, err)
	}
}

func TestUnknownDecisionsAreNotFound(t *testing.T) {
	s := openTemp(t)
	if _, err := s.LoadDecision(bg, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("LoadDecision = %v, want not-found", err)
	}
	if _, _, err := s.RespondDecision(bg, "nope", domain.Response{By: "w", Option: domain.AnswerAllow, At: t0}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RespondDecision = %v, want not-found", err)
	}
}

// #57: an approval without a deadline is never written and never read back.
func TestAnApprovalWithoutADeadlineIsRefused(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	approve(t, a, "d-lost")
	snap := a.Snapshot()
	snap.Decisions[0].Decision.Deadline = time.Time{}
	snap.Decisions[0].Changed = false
	lost, err := domain.Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, lost); !errors.Is(err, ErrNoDeadline) {
		t.Errorf("SaveTask with an approval without a deadline = %v, want ErrNoDeadline", err)
	}
	if _, err := s.LoadTask(bg, "t1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a refused save must store nothing: %v", err)
	}

	// A row that lost its deadline after it was written (a bad migration, a hand edit).
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(bg, `UPDATE decisions SET deadline = 0 WHERE id = 'd-lost'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDecision(bg, "d-lost"); !errors.Is(err, ErrNoDeadline) {
		t.Errorf("LoadDecision of a row without a deadline = %v, want ErrNoDeadline", err)
	}
	if _, err := s.LoadTask(bg, "t1"); !errors.Is(err, ErrNoDeadline) {
		t.Errorf("LoadTask with such a row = %v, want ErrNoDeadline", err)
	}
	if _, err := s.OpenDecisions(bg, "t1"); !errors.Is(err, ErrNoDeadline) {
		t.Errorf("OpenDecisions with such a row = %v, want ErrNoDeadline", err)
	}

	// A question and a review decision may have none.
	b := newAggregate(t, "t2")
	if _, err := b.RaiseDecision(domain.NewDecision{ID: "q1", RunID: "r-t2", Kind: domain.DecisionQuestion, Blocking: true, Now: t0}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, b); err != nil {
		t.Errorf("a question without a deadline was refused: %v", err)
	}
	if _, err := s.LoadDecision(bg, "q1"); err != nil {
		t.Errorf("a question without a deadline did not load: %v", err)
	}
}

// #57: a refusal that changed the Decision is saved with the error.
func TestRespondDecisionSavesWhatARefusalChanged(t *testing.T) {
	setup := func(t *testing.T, id domain.ID, review bool) *Store {
		t.Helper()
		s := openTemp(t)
		a := newAggregate(t, "t1")
		running(t, a)
		if review {
			if _, err := a.PinRevision("r-t1", "agent/topic", "aaa111"); err != nil {
				t.Fatal(err)
			}
			if err := a.StopRun("r-t1"); err != nil {
				t.Fatal(err)
			}
			if err := a.MarkReady(false); err != nil {
				t.Fatal(err)
			}
			if _, err := a.RaiseDecision(domain.NewDecision{ID: id, Kind: domain.DecisionReview, Blocking: true, SHA: "aaa111", Now: t0}); err != nil {
				t.Fatal(err)
			}
		} else {
			approve(t, a, id)
		}
		if _, err := s.SaveTask(bg, a); err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("an answer after the deadline expires the decision for good", func(t *testing.T) {
		s := setup(t, "d1", false)
		stored, _ := s.LoadDecision(bg, "d1")
		got, events, err := s.RespondDecision(bg, "d1", domain.Response{By: "werner", Option: domain.AnswerAllow, At: stored.Deadline})
		if !errors.Is(err, domain.ErrDecisionExpired) {
			t.Fatalf("error = %v, want ErrDecisionExpired", err)
		}
		if got == nil || got.Status != domain.DecisionExpired || len(events) == 0 || events[0].Kind != domain.EventDecisionExpired {
			t.Errorf("decision %+v, events %v", got, eventKinds(events))
		}
		stored, _ = s.LoadDecision(bg, "d1")
		if stored.Status != domain.DecisionExpired || stored.Allows("") {
			t.Errorf("stored status = %s, Allows = %v; the expiry was lost", stored.Status, stored.Allows(""))
		}
		task, _ := s.LoadTask(bg, "t1")
		if task.Task().State != domain.TaskRunning {
			t.Errorf("the expiry left the task %s", task.Task().State)
		}
		// And it stays refused.
		if _, _, err := s.RespondDecision(bg, "d1", domain.Response{By: "werner", Option: domain.AnswerAllow, At: t0.Add(time.Second)}); !errors.Is(err, domain.ErrDecisionClosed) {
			t.Errorf("a second answer = %v, want ErrDecisionClosed", err)
		}
	})

	t.Run("an allow for another commit is stored as a denial", func(t *testing.T) {
		s := setup(t, "d2", true)
		_, events, err := s.RespondDecision(bg, "d2", domain.Response{By: "werner", Option: domain.AnswerAllow, SHA: "bbb222", At: t0.Add(time.Second)})
		if !errors.Is(err, domain.ErrSHAMismatch) {
			t.Fatalf("error = %v, want ErrSHAMismatch", err)
		}
		if len(events) != 1 || events[0].Kind != domain.EventDecisionAnswered {
			t.Errorf("events = %v", eventKinds(events))
		}
		stored, _ := s.LoadDecision(bg, "d2")
		if stored.Status != domain.DecisionAnswered || stored.Answer != domain.AnswerDeny || stored.Allows("aaa111") || stored.Allows("bbb222") {
			t.Errorf("stored = %+v; the denial was lost", stored)
		}
	})

	t.Run("bad input changes nothing", func(t *testing.T) {
		s := setup(t, "d3", false)
		got, events, err := s.RespondDecision(bg, "d3", domain.Response{Option: domain.AnswerAllow, At: t0})
		if !errors.Is(err, domain.ErrDecisionActor) || exitcode.From(err) != exitcode.Usage {
			t.Fatalf("error = %v, want ErrDecisionActor with exit code 2", err)
		}
		if got != nil || len(events) != 0 {
			t.Errorf("got %v, events %v", got, eventKinds(events))
		}
		stored, _ := s.LoadDecision(bg, "d3")
		if stored.Version != 1 || stored.Status != domain.DecisionOpen {
			t.Errorf("version %d, status %s; a rejected answer must not touch the row", stored.Version, stored.Status)
		}
	})

	t.Run("an allowed answer is saved and returned", func(t *testing.T) {
		s := setup(t, "d4", false)
		got, events, err := s.RespondDecision(bg, "d4", domain.Response{By: "werner", Option: domain.AnswerAllow, At: t0.Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if !got.Allows("") || got.Version != 2 || len(events) == 0 {
			t.Errorf("decision %+v, events %v", got, eventKinds(events))
		}
	})
}

// The aggregate carries its Decisions and the run's session ID through the
// store, and writes only the Decisions that changed.
func TestTaskAggregateRoundTripsDecisionsAndSession(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	if err := a.RecordSession("r-t1", "session-1"); err != nil {
		t.Fatal(err)
	}
	approve(t, a, "d1")
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	if d, _ := a.Decision("d1"); d.Version != 1 || d.Changed() {
		t.Errorf("after the save: version %d, changed %v", d.Version, d.Changed())
	}

	got, err := s.LoadTask(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Runs()[0].SessionID != "session-1" || len(got.Decisions()) != 1 || got.Task().State != domain.TaskAwaitingGuidance {
		t.Fatalf("loaded: session %q, %d decisions, task %s", got.Runs()[0].SessionID, len(got.Decisions()), got.Task().State)
	}
	if !reflect.DeepEqual(got.Snapshot(), a.Snapshot()) {
		t.Errorf("the loaded aggregate differs")
	}

	// Saving again without a change does not touch the Decision's version.
	if _, err := s.SaveTask(bg, got); err != nil {
		t.Fatal(err)
	}
	if d, _ := got.Decision("d1"); d.Version != 1 {
		t.Errorf("an unchanged decision was rewritten: version %d", d.Version)
	}

	// A suspended run keeps its cause and reset time.
	reset := t0.Add(2 * time.Hour)
	if err := got.MarkRunning("r-t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := got.SuspendRun("r-t1", domain.CauseQuotaExhausted, reset, "q1", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.LoadTask(bg, "t1")
	loaded, ok := again.Decision("q1")
	if !ok || loaded.Cause != domain.CauseQuotaExhausted || !loaded.ResumeAt.Equal(reset) || len(loaded.Options) != 3 {
		t.Errorf("loaded %+v, want the cause, the reset time and three options", loaded)
	}
	if first, _ := again.Decision("d1"); first.Status != domain.DecisionSuperseded || again.Runs()[0].State != domain.RunPaused || !loaded.Deadline.IsZero() {
		t.Errorf("approval %s, run %s", first.Status, again.Runs()[0].State)
	}
	log, _ := s.EventsSince(bg, "t1", 0, 0)
	n := 0
	for _, e := range log {
		if e.Kind == domain.EventDecisionRaised {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d decision.raised events, want 2: %v", n, eventKinds(log))
	}
}

// RespondDecision goes through the task, so answering the last blocking
// Decision frees the task.
func TestRespondDecisionSettlesTheTask(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	approve(t, a, "d1")
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RespondDecision(bg, "d1", domain.Response{By: "w", Option: domain.AnswerAllow, At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadTask(bg, "t1")
	if d, _ := got.Decision("d1"); got.Task().State != domain.TaskRunning || d.Status != domain.DecisionAnswered {
		t.Errorf("task %s, decision %s", got.Task().State, d.Status)
	}
}

func TestActiveTaskIDsSkipsFinishedTasks(t *testing.T) {
	s := openTemp(t)
	for _, id := range []domain.ID{"t1", "t2", "t3"} {
		if _, err := s.SaveTask(bg, newAggregate(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := s.LoadTask(bg, "t2")
	if err := a.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.ActiveTaskIDs(bg)
	if err != nil || len(got) != 2 || got[0] != "t1" || got[1] != "t3" {
		t.Errorf("active tasks = %v, %v; want t1 and t3", got, err)
	}
}

// A container's address changes on every start, so no table has a column for
// one: the schema is the guarantee that it is never stored (design §5.3).
func TestNoTableStoresAnAddress(t *testing.T) {
	s := openTemp(t)
	tables, err := s.db.QueryContext(bg, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	_ = tables.Close()
	for _, table := range names {
		cols, err := s.db.QueryContext(bg, `SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var c string
			if err := cols.Scan(&c); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{"addr", "ip", "host", "endpoint"} {
				if c == bad || strings.HasPrefix(c, bad+"_") || strings.HasSuffix(c, "_"+bad) {
					t.Errorf("%s.%s looks like a container address column", table, c)
				}
			}
		}
		_ = cols.Close()
	}
}

// Restore and Snapshot are exported, so a caller could forge a state. The store
// refuses to save a change that no event accounts for.
func TestAStateChangeWithNoEventIsRefused(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	approve(t, a, "d1")
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	loaded, _ := s.LoadTask(bg, "t1")

	forge := func(mod func(*domain.Snapshot)) *domain.TaskAggregate {
		snap := loaded.Snapshot()
		for i := range snap.Decisions {
			snap.Decisions[i].Changed = false
		}
		mod(&snap)
		f, err := domain.Restore(snap)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	forgeries := map[string]func(*domain.Snapshot){
		"an open approval turned into an allow": func(s *domain.Snapshot) {
			d := &s.Decisions[0].Decision
			at := d.CreatedAt
			d.Status, d.Answer, d.AnsweredBy, d.AnsweredAt = domain.DecisionAnswered, domain.AnswerAllow, "mallory", &at
		},
		"a task moved to completed": func(s *domain.Snapshot) { s.Task.State = domain.TaskCompleted },
		"a run forced to stopped":   func(s *domain.Snapshot) { s.Runs[0].State = domain.RunStopped },
		"an environment added": func(s *domain.Snapshot) {
			s.Envs = append(s.Envs, domain.Environment{ID: "e-extra", State: domain.EnvRunning})
		},
	}
	for name, mod := range forgeries {
		if _, err := s.SaveTask(bg, forge(mod)); !errors.Is(err, ErrNoEvents) {
			t.Errorf("%s: SaveTask = %v, want ErrNoEvents", name, err)
		}
	}
	got, _ := s.LoadDecision(bg, "d1")
	if got.Status != domain.DecisionOpen || got.Allows("") {
		t.Errorf("a forged answer reached the store: %+v", got)
	}
	// A save with no change at all is still fine.
	if _, err := s.SaveTask(bg, forge(func(*domain.Snapshot) {})); err != nil {
		t.Errorf("an unchanged save: %v", err)
	}
}

// #79: the source tip and the pushed flag of a revision survive a save and a
// load, since a follow-up round rebases from them.
func TestCandidateSourceAndPushedRoundTrip(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	running(t, a)
	if _, err := a.PinPrepared("r-t1", "agent/topic", "aaa111", "src111"); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordPushed("aaa111"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadTask(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := got.LastPushed()
	if !ok || c.SHA != "aaa111" || c.Source != "src111" || !c.Pushed {
		t.Fatalf("loaded LastPushed = %+v, %v", c, ok)
	}
}
