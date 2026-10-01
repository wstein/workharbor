package domain

import (
	"encoding/json"
	"testing"
)

// #79: a follow-up round must rebase only the agent's new commits onto the
// pushed commit, so the candidate remembers the agent's tip it was prepared
// from and whether it was pushed.
func TestPinPreparedRecordsTheSource(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	c, err := a.PinPrepared("r1", "agent/topic", "aaa111", "src111")
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "src111" || c.Pushed {
		t.Fatalf("candidate = %+v, want source src111 and not pushed", c)
	}
	if _, ok := a.LastPushed(); ok {
		t.Fatal("nothing was pushed yet")
	}
	ev := a.TakeEvents()
	var p RevisionPinned
	if err := json.Unmarshal(ev[len(ev)-1].Payload, &p); err != nil || p.Source != "src111" {
		t.Fatalf("pinned event = %s (%v), want the source", ev[len(ev)-1].Payload, err)
	}
}

func TestRecordPushed(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	if _, err := a.PinPrepared("r1", "agent/topic", "aaa111", "src111"); err != nil {
		t.Fatal(err)
	}
	wantNotFound(t, a.RecordPushed("nope"))
	if err := a.RecordPushed("aaa111"); err != nil {
		t.Fatal(err)
	}
	got, ok := a.LastPushed()
	if !ok || got.SHA != "aaa111" || got.Source != "src111" || !got.Pushed {
		t.Fatalf("LastPushed = %+v, %v", got, ok)
	}
	ev := a.TakeEvents()
	if ev[len(ev)-1].Kind != EventRevisionPushed {
		t.Fatalf("last event = %s, want %s", ev[len(ev)-1].Kind, EventRevisionPushed)
	}
	// Recording the same push again changes nothing and records nothing.
	if err := a.RecordPushed("aaa111"); err != nil {
		t.Fatal(err)
	}
	if n := len(a.TakeEvents()); n != 0 {
		t.Fatalf("a repeated push recorded %d events", n)
	}

	// A later revision that is not pushed does not hide the last pushed one.
	if _, err := a.PinPrepared("r1", "agent/topic", "bbb222", "src222"); err != nil {
		t.Fatal(err)
	}
	got, _ = a.LastPushed()
	if got.SHA != "aaa111" {
		t.Fatalf("LastPushed = %s, want aaa111", got.SHA)
	}
}

func TestSnapshotKeepsSourceAndPushed(t *testing.T) {
	a, run, _ := newRunningAggregate(t)
	run.State = RunStopped
	if _, err := a.PinPrepared("r1", "agent/topic", "aaa111", "src111"); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordPushed("aaa111"); err != nil {
		t.Fatal(err)
	}
	b, err := Restore(a.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := b.LastPushed()
	if !ok || got.Source != "src111" || !got.Pushed {
		t.Fatalf("restored LastPushed = %+v, %v", got, ok)
	}
}
