package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func kinds(events []Event) []EventKind {
	var out []EventKind
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func payload[T any](t *testing.T, e Event) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		t.Fatalf("payload of %s: %v", e.Kind, err)
	}
	return v
}

func TestAggregateRecordsTheEventsOfItsChanges(t *testing.T) {
	a, run, env := newRunningAggregate(t)
	// newRunningAggregate started the run: that is the first event.
	if got := kinds(a.TakeEvents()); !reflect.DeepEqual(got, []EventKind{EventRunStarted}) {
		t.Fatalf("events after StartRun = %v", got)
	}
	if again := a.TakeEvents(); len(again) != 0 {
		t.Errorf("TakeEvents must forget what it returned, got %v", kinds(again))
	}

	if err := a.Pause("r1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Resume("r1"); err != nil {
		t.Fatal(err)
	}
	run.State = RunStopped
	if _, err := a.PinRevision("r1", "agent/topic", "aaa111"); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordCI("aaa111", CIPassed); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkReady(true); err != nil {
		t.Fatal(err)
	}
	if err := a.StopEnvironment("e1"); err != nil {
		t.Fatal(err)
	}
	events := a.TakeEvents()
	want := []EventKind{EventRunState, EventRunState, EventRevisionPinned, EventCIRecorded, EventTaskState, EventEnvState}
	if !reflect.DeepEqual(kinds(events), want) {
		t.Fatalf("events = %v, want %v", kinds(events), want)
	}
	for _, e := range events {
		if e.TaskID != "t1" || e.Tier != TierAudit || e.Seq != 0 {
			t.Errorf("event %+v: want task t1, the audit tier and no sequence number yet", e)
		}
	}
	if sc := payload[StateChanged](t, events[0]); sc.Object != "run" || sc.ID != "r1" || sc.From != "running" || sc.To != "paused" {
		t.Errorf("first state change = %+v", sc)
	}
	if sc := payload[StateChanged](t, events[1]); sc.From != "paused" || sc.To != "starting" {
		t.Errorf("resume = %+v, want paused -> starting", sc)
	}
	if rp := payload[RevisionPinned](t, events[2]); rp.SHA != "aaa111" || rp.RunID != "r1" || rp.Branch != "agent/topic" {
		t.Errorf("pinned = %+v", rp)
	}
	if ci := payload[CIRecorded](t, events[3]); ci.SHA != "aaa111" || ci.State != CIPassed {
		t.Errorf("ci = %+v", ci)
	}
	if sc := payload[StateChanged](t, events[4]); sc.Object != "task" || sc.To != "ready_for_review" {
		t.Errorf("task state = %+v", sc)
	}
	if sc := payload[StateChanged](t, events[5]); sc.Object != "environment" || sc.ID != env.ID || sc.To != "stopped" {
		t.Errorf("environment state = %+v", sc)
	}
}

// A refused change produces no event: the audit trail records what happened.
func TestRefusedChangesRecordNothing(t *testing.T) {
	a, _, env := newRunningAggregate(t)
	a.TakeEvents()
	env.State = EnvStopped

	refused := map[string]error{
		"pause with the environment stopped":     a.Pause("r1"),
		"resume of a running run":                a.Resume("r1"),
		"stop of an environment already stopped": a.StopEnvironment("e1"),
		"ready while the run is running":         a.MarkReady(false),
		"pin for an unknown run":                 func() error { _, err := a.PinRevision("nope", "b", "x"); return err }(),
		"CI for an unknown commit":               a.RecordCI("zzz", CIPassed),
		"a run with a duplicate ID":              a.StartRun(&Run{ID: "r1", EnvID: "e1"}),
	}
	for name, err := range refused {
		if err == nil {
			t.Errorf("%s was allowed", name)
		}
	}
	if events := a.TakeEvents(); len(events) != 0 {
		t.Errorf("refused changes produced %v", kinds(events))
	}
}

func TestDecisionRecordsItsEvents(t *testing.T) {
	d, err := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Blocking: true, Subject: "Bash", Input: strings.Repeat("x", MaxDecisionInput+10), Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	events := d.TakeEvents()
	if len(events) != 1 || events[0].Kind != EventDecisionRaised || events[0].TaskID != "t1" || !events[0].At.Equal(t0) {
		t.Fatalf("raised events = %+v", events)
	}
	raised := payload[DecisionRaised](t, events[0])
	if raised.Subject != "Bash" || raised.RunID != "r1" || !raised.Blocking || len([]rune(raised.Input)) != MaxDecisionInput || !raised.Deadline.Equal(d.Deadline) {
		t.Errorf("raised payload = %+v: the audit entry carries the tool and the capped input", raised)
	}

	at := t0.Add(time.Minute)
	if err := d.Respond(Response{By: "werner", Option: AnswerAllow, Reason: "fine", At: at}); err != nil {
		t.Fatal(err)
	}
	events = d.TakeEvents()
	if len(events) != 1 || events[0].Kind != EventDecisionAnswered || !events[0].At.Equal(at) {
		t.Fatalf("answered events = %+v", events)
	}
	if ans := payload[AnswerRecorded](t, events[0]); ans.Answer != AnswerAllow || ans.By != "werner" || ans.Reason != "fine" {
		t.Errorf("answered payload = %+v", ans)
	}
	if len(d.TakeEvents()) != 0 {
		t.Error("TakeEvents must forget what it returned")
	}
}

func TestDecisionRecordsDenialExpiryAndSupersession(t *testing.T) {
	// An allow for another commit is recorded as the denial it became.
	review, _ := Raise(NewDecision{ID: "d2", TaskID: "t1", Kind: DecisionReview, SHA: "aaa111", Now: t0})
	review.TakeEvents()
	_ = review.Respond(Response{By: "werner", Option: AnswerAllow, SHA: "bbb222", At: t0.Add(time.Second)})
	events := review.TakeEvents()
	if len(events) != 1 || payload[AnswerRecorded](t, events[0]).Answer != AnswerDeny {
		t.Errorf("a mismatching allow must be recorded as a denial: %+v", events)
	}

	// A late answer records the expiry, not the answer.
	late, _ := Raise(NewDecision{ID: "d3", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Now: t0})
	late.TakeEvents()
	_ = late.Respond(Response{By: "werner", Option: AnswerAllow, At: late.Deadline})
	if got := kinds(late.TakeEvents()); !reflect.DeepEqual(got, []EventKind{EventDecisionExpired}) {
		t.Errorf("a late answer recorded %v, want only the expiry", got)
	}

	expired, _ := Raise(NewDecision{ID: "d4", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Now: t0})
	expired.TakeEvents()
	if !expired.Expire(expired.Deadline) {
		t.Fatal("setup: the decision should expire")
	}
	if got := kinds(expired.TakeEvents()); !reflect.DeepEqual(got, []EventKind{EventDecisionExpired}) {
		t.Errorf("Expire recorded %v", got)
	}

	sup, _ := Raise(NewDecision{ID: "d5", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Now: t0})
	sup.TakeEvents()
	if err := sup.Supersede(); err != nil {
		t.Fatal(err)
	}
	n, err := sup.Reraise("d5b", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(sup.TakeEvents()); !reflect.DeepEqual(got, []EventKind{EventDecisionSuperseded, EventDecisionReraised}) {
		t.Errorf("the superseded decision recorded %v", got)
	}
	if got := kinds(n.TakeEvents()); !reflect.DeepEqual(got, []EventKind{EventDecisionRaised}) {
		t.Errorf("the raised-again decision recorded %v", got)
	}

	// Refused answers record nothing.
	d, _ := Raise(NewDecision{ID: "d6", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Now: t0})
	d.TakeEvents()
	_ = d.Respond(Response{Option: AnswerAllow, At: t0.Add(time.Second)})      // no actor
	_ = d.Respond(Response{By: "w", Option: "maybe", At: t0.Add(time.Second)}) // unknown option
	_ = d.Respond(Response{By: "w", Option: AnswerAllow})                      // no time
	if events := d.TakeEvents(); len(events) != 0 {
		t.Errorf("refused answers recorded %v", kinds(events))
	}
}

func TestPendingEventsDoNotForget(t *testing.T) {
	a, _, _ := newRunningAggregate(t)
	first := a.PendingEvents()
	if len(first) != 1 || len(a.PendingEvents()) != 1 {
		t.Fatalf("PendingEvents must not drain: %d then %d", len(first), len(a.PendingEvents()))
	}
	first[0].Kind = "tampered"
	if a.PendingEvents()[0].Kind == "tampered" {
		t.Error("PendingEvents must return a copy")
	}
	if got := a.TakeEvents(); len(got) != 1 || len(a.PendingEvents()) != 0 {
		t.Errorf("TakeEvents drains: %d taken, %d pending after", len(got), len(a.PendingEvents()))
	}

	d, _ := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Now: t0})
	if first, second := len(d.PendingEvents()), len(d.PendingEvents()); first != 1 || second != 1 {
		t.Errorf("a Decision's PendingEvents must not drain: %d then %d", first, second)
	}
	d.TakeEvents()
	if len(d.PendingEvents()) != 0 {
		t.Error("TakeEvents drains a Decision too")
	}
}
