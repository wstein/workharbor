package store

import (
	"context"
	"errors"
	"reflect"
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
	a.AddEnvironment(&domain.Environment{ID: domain.ID("e-" + string(id)), Backend: "apple", State: domain.EnvRunning})
	if err := a.StartRun(&domain.Run{ID: domain.ID("r-" + string(id)), WorkspaceID: "w1", EnvID: domain.ID("e-" + string(id))}); err != nil {
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

func TestSaveAndLoadATaskRoundTrips(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	a.Runs[0].State = domain.RunRunning
	if err := a.Pause("r-t1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Resume("r-t1"); err != nil {
		t.Fatal(err)
	}
	a.Runs[0].State = domain.RunStopped
	if _, err := a.PinRevision("r-t1", "agent/topic", "aaa111"); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordCI("aaa111", domain.CIPassed); err != nil {
		t.Fatal(err)
	}
	a.Candidates[0].PRURL = "https://github.com/wstein/workharbor/pull/22"

	events, err := s.SaveTask(bg, a)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.EventKind{domain.EventRunStarted, domain.EventRunState, domain.EventRunState, domain.EventRevisionPinned, domain.EventCIRecorded}
	if !reflect.DeepEqual(eventKinds(events), want) {
		t.Errorf("saved events = %v, want %v", eventKinds(events), want)
	}
	if a.Task.Version != 1 || len(a.PendingEvents()) != 0 {
		t.Errorf("after the save: version %d, %d pending events; want 1 and none", a.Task.Version, len(a.PendingEvents()))
	}

	got, err := s.LoadTask(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Task, a.Task) || !reflect.DeepEqual(got.Runs, a.Runs) || !reflect.DeepEqual(got.Envs, a.Envs) || !reflect.DeepEqual(got.Candidates, a.Candidates) {
		t.Errorf("the loaded aggregate differs:\n got %+v\nwant %+v", got, a)
	}
	log, _ := s.EventsSince(bg, "t1", 0, 0)
	if !reflect.DeepEqual(eventKinds(log), want) || log[0].Seq != 1 {
		t.Errorf("the log = %v", eventKinds(log))
	}
}

func TestSavingAgainBumpsTheVersionAndAppendsOnlyNewEvents(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	a.Runs[0].State = domain.RunRunning
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
	if a.Task.Version != 2 || len(events) != 1 || events[0].Kind != domain.EventRunState {
		t.Errorf("version %d, events %v; want 2 and one run.state", a.Task.Version, eventKinds(events))
	}
	// A save with nothing new changes the version but adds no event.
	if events, err = s.SaveTask(bg, a); err != nil || len(events) != 0 || a.Task.Version != 3 {
		t.Errorf("an unchanged save: %d events, version %d, %v", len(events), a.Task.Version, err)
	}
	loaded, _ := s.LoadTask(bg, "t1")
	if loaded.Runs[0].State != domain.RunPaused || loaded.Task.Version != 3 {
		t.Errorf("loaded run %s at version %d", loaded.Runs[0].State, loaded.Task.Version)
	}
}

// Two writers load the same task: the second to save loses and writes nothing.
func TestCompareAndSwapOnATask(t *testing.T) {
	s := openTemp(t)
	a := newAggregate(t, "t1")
	a.Runs[0].State = domain.RunRunning
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

	second.Runs[0].State = domain.RunStopped
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
	if stored.Runs[0].State != domain.RunPaused || len(stored.Candidates) != 0 {
		t.Errorf("the winner's state was overwritten: run %s, %d candidates", stored.Runs[0].State, len(stored.Candidates))
	}
	if second.Task.Version != 1 || len(second.PendingEvents()) == 0 {
		t.Errorf("a lost save must leave the aggregate to retry: version %d, %d pending events", second.Task.Version, len(second.PendingEvents()))
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

func TestUnknownTasksAreNotFound(t *testing.T) {
	s := openTemp(t)
	_, err := s.LoadTask(bg, "nope")
	if !errors.Is(err, domain.ErrNotFound) || exitcode.From(err) != exitcode.NotFound {
		t.Errorf("LoadTask = %v (exit %d), want not-found", err, exitcode.From(err))
	}
	a := newAggregate(t, "ghost")
	a.Task.Version = 5 // it was loaded from somewhere that no longer has it
	if _, err := s.SaveTask(bg, a); !errors.Is(err, domain.ErrNotFound) {
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
	if a.Task.Version != 0 || len(a.PendingEvents()) != 1 {
		t.Errorf("a rolled-back save must leave the aggregate as it was: version %d, %d pending", a.Task.Version, len(a.PendingEvents()))
	}
}

func raiseApproval(t *testing.T, id domain.ID) *domain.Decision {
	t.Helper()
	d, err := domain.Raise(domain.NewDecision{
		ID: id, TaskID: "t1", RunID: "r-t1", Kind: domain.DecisionApproval, Blocking: true,
		Subject: "Bash", Input: "make deploy", Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSaveAndLoadADecisionRoundTrips(t *testing.T) {
	s := openTemp(t)
	if _, err := s.SaveTask(bg, newAggregate(t, "t1")); err != nil {
		t.Fatal(err)
	}
	d := raiseApproval(t, "d1")
	if _, err := s.SaveDecision(bg, d); err != nil {
		t.Fatal(err)
	}
	if err := d.Respond(domain.Response{By: "werner", Option: domain.AnswerAllow, Reason: "fine", At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	events, err := s.SaveDecision(bg, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != domain.EventDecisionAnswered || d.Version != 2 {
		t.Errorf("events %v, version %d", eventKinds(events), d.Version)
	}

	got, err := s.LoadDecision(bg, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Errorf("the loaded decision differs:\n got %+v\nwant %+v", got, d)
	}
	if !got.Allows("") || got.AnsweredBy != "werner" || got.AnsweredAt == nil || !got.AnsweredAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("an answered allow must survive the store: %+v", got)
	}
	log, _ := s.EventsSince(bg, "t1", 0, 0)
	// The task's own events come first, then the decision's: raised, answered.
	if kinds := eventKinds(log); kinds[len(kinds)-2] != domain.EventDecisionRaised || kinds[len(kinds)-1] != domain.EventDecisionAnswered {
		t.Errorf("log = %v", kinds)
	}
}

// An answer and an expiry race: whoever saves first wins, and the other is told.
func TestAnAnswerAndAnExpiryCannotBothWin(t *testing.T) {
	for _, answerFirst := range []bool{true, false} {
		s := openTemp(t)
		if _, err := s.SaveTask(bg, newAggregate(t, "t1")); err != nil {
			t.Fatal(err)
		}
		d := raiseApproval(t, "d1")
		if _, err := s.SaveDecision(bg, d); err != nil {
			t.Fatal(err)
		}
		answerer, _ := s.LoadDecision(bg, "d1")
		expirer, _ := s.LoadDecision(bg, "d1")
		if err := answerer.Respond(domain.Response{By: "werner", Option: domain.AnswerAllow, At: t0.Add(time.Minute)}); err != nil {
			t.Fatal(err)
		}
		if !expirer.Expire(expirer.Deadline) {
			t.Fatal("setup: the decision should expire")
		}

		order := []*domain.Decision{answerer, expirer}
		wantStatus := domain.DecisionAnswered
		if !answerFirst {
			order = []*domain.Decision{expirer, answerer}
			wantStatus = domain.DecisionExpired
		}
		if _, err := s.SaveDecision(bg, order[0]); err != nil {
			t.Fatalf("the first save: %v", err)
		}
		loser := order[1]
		if _, err := s.SaveDecision(bg, loser); !errors.Is(err, ErrStale) {
			t.Fatalf("answerFirst=%v: the second save = %v, want ErrStale", answerFirst, err)
		}
		if len(loser.PendingEvents()) == 0 || loser.Version != 1 {
			t.Errorf("the loser must keep its change to retry: version %d, %d pending", loser.Version, len(loser.PendingEvents()))
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
	for _, id := range []domain.ID{"t1", "t2"} {
		if _, err := s.SaveTask(bg, newAggregate(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	open := raiseApproval(t, "d-open")
	answered := raiseApproval(t, "d-answered")
	if err := answered.Respond(domain.Response{By: "w", Option: domain.AnswerDeny, At: t0.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	other := raiseApproval(t, "d-other")
	other.TaskID = "t2"
	for _, d := range []*domain.Decision{open, answered, other} {
		if _, err := s.SaveDecision(bg, d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.OpenDecisions(bg, "t1")
	if err != nil || len(got) != 1 || got[0].ID != "d-open" {
		t.Errorf("open decisions of t1 = %+v, %v", got, err)
	}
}

func TestADecisionNeedsItsTaskAndUnknownOnesAreNotFound(t *testing.T) {
	s := openTemp(t)
	_, err := s.SaveDecision(bg, raiseApproval(t, "d1")) // task t1 was never saved
	if err == nil || errors.Is(err, ErrStale) {
		t.Errorf("a decision for an unknown task = %v, want a foreign key error", err)
	}
	if _, err := s.LoadDecision(bg, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("LoadDecision = %v, want not-found", err)
	}
	d := raiseApproval(t, "gone")
	d.Version = 3
	if _, err := s.SaveDecision(bg, d); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("saving a loaded decision that is gone = %v, want not-found", err)
	}
}

// #57: an approval without a deadline is never written and never read back.
func TestAnApprovalWithoutADeadlineIsRefused(t *testing.T) {
	s := openTemp(t)
	if _, err := s.SaveTask(bg, newAggregate(t, "t1")); err != nil {
		t.Fatal(err)
	}

	lost := raiseApproval(t, "d-lost")
	lost.Deadline = time.Time{}
	if _, err := s.SaveDecision(bg, lost); !errors.Is(err, ErrNoDeadline) {
		t.Errorf("SaveDecision without a deadline = %v, want ErrNoDeadline", err)
	}
	if _, err := s.LoadDecision(bg, "d-lost"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a refused approval must not be stored: %v", err)
	}

	// A row that lost its deadline after it was written (a bad migration, a hand edit).
	if _, err := s.SaveDecision(bg, raiseApproval(t, "d1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(bg, `UPDATE decisions SET deadline = 0 WHERE id = 'd1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDecision(bg, "d1"); !errors.Is(err, ErrNoDeadline) {
		t.Errorf("LoadDecision of a row without a deadline = %v, want ErrNoDeadline", err)
	}
	if _, err := s.OpenDecisions(bg, "t1"); !errors.Is(err, ErrNoDeadline) {
		t.Errorf("OpenDecisions with such a row = %v, want ErrNoDeadline", err)
	}

	// A question and a review decision may have none.
	q, err := domain.Raise(domain.NewDecision{ID: "q1", TaskID: "t1", RunID: "r-t1", Kind: domain.DecisionQuestion, Blocking: true, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDecision(bg, q); err != nil {
		t.Errorf("a question without a deadline was refused: %v", err)
	}
	if _, err := s.LoadDecision(bg, "q1"); err != nil {
		t.Errorf("a question without a deadline did not load: %v", err)
	}
}

// #57: a refusal that changed the Decision is saved with the error.
func TestRespondDecisionSavesWhatARefusalChanged(t *testing.T) {
	setup := func(t *testing.T, d *domain.Decision) *Store {
		t.Helper()
		s := openTemp(t)
		if _, err := s.SaveTask(bg, newAggregate(t, "t1")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveDecision(bg, d); err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("an answer after the deadline expires the decision for good", func(t *testing.T) {
		d := raiseApproval(t, "d1")
		s := setup(t, d)
		got, events, err := s.RespondDecision(bg, "d1", domain.Response{By: "werner", Option: domain.AnswerAllow, At: d.Deadline})
		if !errors.Is(err, domain.ErrDecisionExpired) {
			t.Fatalf("error = %v, want ErrDecisionExpired", err)
		}
		if got == nil || got.Status != domain.DecisionExpired || len(events) != 1 || events[0].Kind != domain.EventDecisionExpired {
			t.Errorf("decision %+v, events %v", got, eventKinds(events))
		}
		stored, _ := s.LoadDecision(bg, "d1")
		if stored.Status != domain.DecisionExpired || stored.Allows("") {
			t.Errorf("stored status = %s, Allows = %v; the expiry was lost", stored.Status, stored.Allows(""))
		}
		// And it stays refused.
		if _, _, err := s.RespondDecision(bg, "d1", domain.Response{By: "werner", Option: domain.AnswerAllow, At: t0.Add(time.Second)}); !errors.Is(err, domain.ErrDecisionClosed) {
			t.Errorf("a second answer = %v, want ErrDecisionClosed", err)
		}
	})

	t.Run("an allow for another commit is stored as a denial", func(t *testing.T) {
		review, err := domain.Raise(domain.NewDecision{ID: "d2", TaskID: "t1", Kind: domain.DecisionReview, Blocking: true, SHA: "aaa111", Now: t0})
		if err != nil {
			t.Fatal(err)
		}
		s := setup(t, review)
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
		d := raiseApproval(t, "d3")
		s := setup(t, d)
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
		d := raiseApproval(t, "d4")
		s := setup(t, d)
		got, events, err := s.RespondDecision(bg, "d4", domain.Response{By: "werner", Option: domain.AnswerAllow, At: t0.Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if !got.Allows("") || got.Version != 2 || len(events) != 1 {
			t.Errorf("decision %+v, events %v", got, eventKinds(events))
		}
	})

	t.Run("an unknown decision", func(t *testing.T) {
		s := setup(t, raiseApproval(t, "d5"))
		if _, _, err := s.RespondDecision(bg, "nope", domain.Response{By: "w", Option: domain.AnswerAllow, At: t0}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})
}
