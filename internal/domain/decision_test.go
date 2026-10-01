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

// #49: an approval must never be raised without a deadline.
func TestRaiseRejectsNegativeTimeoutAndZeroNow(t *testing.T) {
	for _, kind := range []DecisionKind{DecisionApproval, DecisionQuestion} {
		spec := NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: kind, Timeout: -time.Hour, Now: t0}
		if _, err := Raise(spec); !errors.Is(err, ErrDecisionTimeout) {
			t.Errorf("%s with a negative timeout: error = %v, want ErrDecisionTimeout", kind, err)
		}
		spec = NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: kind, Now: time.Time{}}
		if _, err := Raise(spec); !errors.Is(err, ErrDecisionTime) {
			t.Errorf("%s with a zero Now: error = %v, want ErrDecisionTime", kind, err)
		}
	}
}

func TestEveryApprovalHasADeadline(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Nanosecond, time.Minute, 24 * time.Hour} {
		d, err := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Timeout: timeout, Now: t0})
		if err != nil {
			t.Fatalf("timeout %v: %v", timeout, err)
		}
		if d.Deadline.IsZero() || !d.Deadline.After(t0) {
			t.Errorf("timeout %v: deadline = %v, want a deadline after the start", timeout, d.Deadline)
		}
	}
}

// A year-late allow must not count, whatever timeout the approval was raised with.
func TestAYearLateAllowNeverCounts(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Minute} {
		d, err := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Timeout: timeout, Now: t0})
		if err != nil {
			t.Fatal(err)
		}
		err = d.Respond(Response{By: "werner", Option: AnswerAllow, At: t0.AddDate(1, 0, 0)})
		if !errors.Is(err, ErrDecisionExpired) || d.Allows("") {
			t.Errorf("timeout %v: late allow gave %v, allows %v", timeout, err, d.Allows(""))
		}
	}
}

// #49: every status change goes through the transition table.
func TestStatusMovesThroughTheTable(t *testing.T) {
	for _, from := range allDecisionStatuses {
		for _, to := range allDecisionStatuses {
			d := &Decision{ID: "d1", Status: from}
			err := d.move(to)
			if from.CanTransition(to) {
				if err != nil || d.Status != to {
					t.Errorf("%s -> %s: err %v, status %s", from, to, err, d.Status)
				}
				continue
			}
			var ce *ConflictError
			if !errors.As(err, &ce) || ce.Rule != RuleTransition {
				t.Errorf("%s -> %s: error = %v, want a transition conflict", from, to, err)
			}
			if d.Status != from {
				t.Errorf("%s -> %s: a refused move changed the status to %s", from, to, d.Status)
			}
		}
	}
}

// Respond, Expire and Supersede must not set Status behind the table's back:
// with the table emptied none of them may change the status.
func TestResolutionUsesTheTable(t *testing.T) {
	saved := decisionTransitions
	decisionTransitions = map[DecisionStatus][]DecisionStatus{}
	t.Cleanup(func() { decisionTransitions = saved })

	d, err := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Respond(Response{By: "werner", Option: AnswerAllow, At: t0.Add(time.Second)})
	if d.Status != DecisionOpen || d.Allows("") {
		t.Errorf("Respond changed the status to %s without the table", d.Status)
	}
	_ = d.Respond(Response{By: "werner", Option: AnswerAllow, At: d.Deadline})
	if d.Status != DecisionOpen {
		t.Errorf("a late Respond changed the status to %s without the table", d.Status)
	}
	if d.Expire(d.Deadline.Add(time.Hour)) || d.Status != DecisionOpen {
		t.Errorf("Expire changed the status to %s without the table", d.Status)
	}
	_ = d.Supersede()
	if d.Status != DecisionOpen {
		t.Errorf("Supersede changed the status to %s without the table", d.Status)
	}
}
