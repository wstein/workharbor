package domain

import "testing"

func TestRecordPR(t *testing.T) {
	a := newStoppedAggregate(t)
	a.TakeEvents()
	if err := a.RecordPR("aaa111", "https://example.test/pull/1"); err != nil {
		t.Fatal(err)
	}
	if c, _ := a.CurrentCandidate(); c.PRURL != "https://example.test/pull/1" || len(a.PendingEvents()) != 1 || a.PendingEvents()[0].Kind != EventPRRecorded {
		t.Errorf("candidate %+v, events %v", c, a.PendingEvents())
	}
	a.TakeEvents()
	if err := a.RecordPR("aaa111", "https://example.test/pull/1"); err != nil || len(a.PendingEvents()) != 0 {
		t.Errorf("the same URL again: %v, %d events", err, len(a.PendingEvents()))
	}
	wantConflict(t, a.RecordPR("aaa111", "https://example.test/pull/2"), RulePinnedSHA)
	wantConflict(t, a.RecordPR("aaa111", ""), RulePinnedSHA)
	wantNotFound(t, a.RecordPR("zzz999", "https://example.test/pull/3"))
}
