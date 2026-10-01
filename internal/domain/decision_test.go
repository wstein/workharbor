package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

var allDecisionStatuses = []DecisionStatus{DecisionOpen, DecisionAnswered, DecisionExpired, DecisionSuperseded}

func TestDecisionStatusTransitionsExhaustive(t *testing.T) {
	legal := map[[2]DecisionStatus]bool{
		{DecisionOpen, DecisionAnswered}:   true,
		{DecisionOpen, DecisionExpired}:    true,
		{DecisionOpen, DecisionSuperseded}: true,
	}
	for _, from := range allDecisionStatuses {
		for _, to := range allDecisionStatuses {
			if got := from.CanTransition(to); got != legal[[2]DecisionStatus{from, to}] {
				t.Errorf("%s -> %s: CanTransition = %v, want %v", from, to, got, !got)
			}
		}
		if got, want := from.Terminal(), from != DecisionOpen; got != want {
			t.Errorf("%s: Terminal = %v, want %v", from, got, want)
		}
	}
}

func TestRaise(t *testing.T) {
	tests := []struct {
		name string
		spec NewDecision
		err  error
	}{
		{"question from a run", NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Blocking: true, Now: t0}, nil},
		{"approval from a run", NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Blocking: true, Now: t0}, nil},
		{"review of a commit", NewDecision{ID: "d1", TaskID: "t1", Kind: DecisionReview, SHA: "8e2f1c4", Now: t0}, nil},
		{"no id", NewDecision{TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Now: t0}, ErrDecisionID},
		{"no task", NewDecision{ID: "d1", RunID: "r1", Kind: DecisionQuestion, Now: t0}, ErrDecisionID},
		{"unknown kind", NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: "poll", Now: t0}, ErrDecisionKind},
		{"approval without a run", NewDecision{ID: "d1", TaskID: "t1", Kind: DecisionApproval, Now: t0}, ErrDecisionRun},
		{"question without a run", NewDecision{ID: "d1", TaskID: "t1", Kind: DecisionQuestion, Now: t0}, ErrDecisionRun},
		{"review with a run", NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionReview, SHA: "8e2f1c4", Now: t0}, ErrDecisionRun},
		{"review without a commit", NewDecision{ID: "d1", TaskID: "t1", Kind: DecisionReview, Now: t0}, ErrDecisionSHA},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Raise(tc.spec)
			if !errors.Is(err, tc.err) {
				t.Fatalf("Raise error = %v, want %v", err, tc.err)
			}
			if tc.err == nil && d.Status != DecisionOpen {
				t.Errorf("status = %s, want open", d.Status)
			}
		})
	}
}

func TestApprovalGetsDeadlineAndOptions(t *testing.T) {
	d, err := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Blocking: true, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(DefaultApprovalTimeout); !d.Deadline.Equal(want) {
		t.Errorf("deadline = %v, want %v", d.Deadline, want)
	}
	if len(d.Options) != 2 || d.Options[0] != AnswerAllow || d.Options[1] != AnswerDeny {
		t.Errorf("options = %v, want allow and deny", d.Options)
	}

	custom, err := Raise(NewDecision{ID: "d2", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Timeout: time.Minute, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(time.Minute); !custom.Deadline.Equal(want) {
		t.Errorf("deadline = %v, want %v", custom.Deadline, want)
	}

	q, err := Raise(NewDecision{ID: "d3", TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if !q.Deadline.IsZero() {
		t.Errorf("a question has no default deadline, got %v", q.Deadline)
	}
}

func TestInputIsCapped(t *testing.T) {
	long := strings.Repeat("é", MaxDecisionInput+500) // multi-byte characters
	d, err := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Input: long, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(d.Input)); got != MaxDecisionInput {
		t.Errorf("input has %d characters, want %d", got, MaxDecisionInput)
	}
	if !d.InputTruncated {
		t.Error("InputTruncated = false for a capped input")
	}

	exact := strings.Repeat("a", MaxDecisionInput)
	d, _ = Raise(NewDecision{ID: "d2", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Input: exact, Now: t0})
	if d.Input != exact || d.InputTruncated {
		t.Error("an input of exactly the cap must be kept whole")
	}
}

func TestRaisesGuidance(t *testing.T) {
	// D13: only a blocking Decision raised by a live run moves the task to
	// awaiting_guidance; "Ready to push?" is a review Decision and does not.
	tests := []struct {
		name string
		spec NewDecision
		want bool
	}{
		{"blocking question", NewDecision{ID: "d", TaskID: "t", RunID: "r", Kind: DecisionQuestion, Blocking: true, Now: t0}, true},
		{"blocking approval", NewDecision{ID: "d", TaskID: "t", RunID: "r", Kind: DecisionApproval, Blocking: true, Now: t0}, true},
		{"non-blocking question", NewDecision{ID: "d", TaskID: "t", RunID: "r", Kind: DecisionQuestion, Now: t0}, false},
		{"ready to push", NewDecision{ID: "d", TaskID: "t", Kind: DecisionReview, Blocking: true, SHA: "8e2f1c4", Now: t0}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Raise(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got := d.RaisesGuidance(); got != tc.want {
				t.Errorf("RaisesGuidance = %v, want %v", got, tc.want)
			}
		})
	}
}
