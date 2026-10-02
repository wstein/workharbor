package store

import (
	"errors"
	"testing"
	"time"
)

func TestAChangeIsAppliedOnceAndAuditedWithWhoConfirmedIt(t *testing.T) {
	s := openTemp(t)
	at := time.Unix(1_700_000_000, 0)
	old := WorkflowRecord{Workflow: "integration", Branch: "develop"}
	next := WorkflowRecord{Workflow: "prototype", Branch: "scratch"}
	if _, _, err := s.ApplyWorkflow(bg, "o/r", old, "serve", at); err != nil {
		t.Fatal(err)
	}
	c, err := s.RaiseChange(bg, "c1", "O/R", old, next, at)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.OpenChanges(bg); len(got) != 1 || got[0].ID != "c1" || got[0].To != next {
		t.Fatalf("open = %+v", got)
	}
	// Raised again with the same difference (a restart) it is the same change.
	if again, err := s.RaiseChange(bg, "c2", "o/r", old, next, at.Add(time.Hour)); err != nil || again.ID != c.ID {
		t.Fatalf("raised again = %+v, %v; want %s", again, err, c.ID)
	}
	if _, err := s.ConfirmChange(bg, "c1", "web+passkey", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if rec, _, _ := s.RecordedWorkflow(bg, "o/r"); rec != next {
		t.Errorf("recorded = %+v, want %+v", rec, next)
	}
	log, _ := s.WorkflowChanges(bg, "o/r")
	if len(log) != 1 || log[0].ConfirmedBy != "web+passkey" || log[0].To != "prototype" {
		t.Errorf("audit = %+v", log)
	}
	if got, _ := s.OpenChanges(bg); len(got) != 0 {
		t.Errorf("still open: %+v", got)
	}
	// It cannot be confirmed twice, which is also the replay of an answer.
	if _, err := s.ConfirmChange(bg, "c1", "web+passkey", at); !errors.Is(err, ErrNoChange) {
		t.Errorf("second confirm = %v, want ErrNoChange", err)
	}
}

func TestAChangeRaisedAgainstAnOldWorkflowIsStale(t *testing.T) {
	s := openTemp(t)
	at := time.Unix(1_700_000_000, 0)
	old := WorkflowRecord{Workflow: "integration", Branch: "develop"}
	mid := WorkflowRecord{Workflow: "published"}
	next := WorkflowRecord{Workflow: "prototype", Branch: "scratch"}
	if _, _, err := s.ApplyWorkflow(bg, "o/r", old, "serve", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RaiseChange(bg, "c1", "o/r", old, next, at); err != nil {
		t.Fatal(err)
	}
	// The host confirms another change in between.
	if _, _, err := s.ApplyWorkflow(bg, "o/r", mid, "host-cli", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmChange(bg, "c1", "web+passkey", at); !errors.Is(err, ErrStaleChange) {
		t.Fatalf("confirm = %v, want ErrStaleChange", err)
	}
	if rec, _, _ := s.RecordedWorkflow(bg, "o/r"); rec != mid {
		t.Errorf("a stale change moved the record to %+v", rec)
	}
	if got, _ := s.OpenChanges(bg); len(got) != 0 {
		t.Errorf("a stale change stays open: %+v", got)
	}
}

func TestADifferentChangeWithdrawsTheOpenOneAndAnUnaskedOneIsWithdrawn(t *testing.T) {
	s := openTemp(t)
	at := time.Unix(1_700_000_000, 0)
	old := WorkflowRecord{Workflow: "integration", Branch: "develop"}
	if _, _, err := s.ApplyWorkflow(bg, "o/r", old, "serve", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RaiseChange(bg, "c1", "o/r", old, WorkflowRecord{Workflow: "published"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RaiseChange(bg, "c2", "o/r", old, WorkflowRecord{Workflow: "prototype", Branch: "x"}, at); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.OpenChanges(bg); len(got) != 1 || got[0].ID != "c2" {
		t.Fatalf("open = %+v, want only c2", got)
	}
	if n, err := s.WithdrawChangesExcept(bg, nil, at); err != nil || n != 1 {
		t.Fatalf("withdrawn %d, %v; want 1", n, err)
	}
	if _, err := s.ConfirmChange(bg, "c2", "web+passkey", at); !errors.Is(err, ErrNoChange) {
		t.Errorf("a withdrawn change was confirmed: %v", err)
	}
}
