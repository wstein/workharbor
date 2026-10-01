package domain

import (
	"errors"
	"testing"
	"time"
)

func newApproval(t *testing.T) *Decision {
	t.Helper()
	d, err := Raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Blocking: true, Subject: "Bash", Input: "rm -rf build", Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newPushReview(t *testing.T, sha string) *Decision {
	t.Helper()
	d, err := Raise(NewDecision{ID: "d2", TaskID: "t1", Kind: DecisionReview, Blocking: true, SHA: sha, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func allow(at time.Time, sha string) Response {
	return Response{By: "werner", Option: AnswerAllow, SHA: sha, At: at}
}

func TestAllowedApprovalAllows(t *testing.T) {
	d := newApproval(t)
	if err := d.Respond(Response{By: "werner", Option: AnswerAllow, Reason: "fine", At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if d.Status != DecisionAnswered || d.Answer != AnswerAllow || d.AnsweredBy != "werner" || d.Reason != "fine" {
		t.Errorf("decision = %+v", d)
	}
	if d.AnsweredAt == nil || !d.AnsweredAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("AnsweredAt = %v", d.AnsweredAt)
	}
	if !d.Allows("") {
		t.Error("an answered allow must allow")
	}
}

// Fail closed, criterion 1: a timeout denies.
func TestTimeoutDenies(t *testing.T) {
	d := newApproval(t)
	deadline := d.Deadline

	if d.Allows("") {
		t.Fatal("an open approval must not allow")
	}
	if d.Expire(deadline.Add(-time.Second)) {
		t.Fatal("must not expire before the deadline")
	}
	if !d.Expire(deadline) {
		t.Fatal("must expire at the deadline")
	}
	if d.Expire(deadline.Add(time.Hour)) {
		t.Error("expiring twice must change nothing")
	}
	if d.Status != DecisionExpired || d.Allows("") {
		t.Errorf("an expired approval must deny: status %s, allows %v", d.Status, d.Allows(""))
	}
	if err := d.Respond(allow(deadline.Add(time.Second), "")); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("answering an expired decision: %v, want ErrDecisionClosed", err)
	}
}

func TestLateAnswerNeverCounts(t *testing.T) {
	// The reconciler has not run yet, so the Decision is still open in the
	// store, but the answer arrives after the deadline.
	for _, late := range []time.Duration{0, time.Second, time.Hour} {
		d := newApproval(t)
		err := d.Respond(allow(d.Deadline.Add(late), ""))
		if !errors.Is(err, ErrDecisionExpired) {
			t.Fatalf("late by %v: error = %v, want ErrDecisionExpired", late, err)
		}
		if d.Status != DecisionExpired || d.Allows("") || d.Answer != "" {
			t.Errorf("late by %v: a late allow was kept: status %s, answer %q", late, d.Status, d.Answer)
		}
	}
}

// Fail closed, criterion 2: a restart supersedes an open approval and the ask
// is raised again on resume.
func TestRestartSupersedesAndTheAskIsRaisedAgain(t *testing.T) {
	d := newApproval(t)
	if err := d.Supersede(); err != nil {
		t.Fatal(err)
	}
	if d.Status != DecisionSuperseded || d.Allows("") {
		t.Fatalf("a superseded approval must deny: %s", d.Status)
	}
	if err := d.Respond(allow(t0.Add(time.Minute), "")); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("answering a superseded decision: %v, want ErrDecisionClosed", err)
	}

	resumed := t0.Add(2 * time.Minute)
	n, err := d.Reraise("d1b", resumed)
	if err != nil {
		t.Fatal(err)
	}
	if n.Status != DecisionOpen || n.ID != "d1b" || n.RunID != "r1" || n.TaskID != "t1" {
		t.Errorf("raised again = %+v", n)
	}
	if n.Subject != d.Subject || n.Input != d.Input || n.Kind != d.Kind || !n.Blocking {
		t.Errorf("the ask changed: %+v", n)
	}
	if want := resumed.Add(DefaultApprovalTimeout); !n.Deadline.Equal(want) {
		t.Errorf("deadline = %v, want a fresh one at %v", n.Deadline, want)
	}
	if d.SupersededBy != "d1b" {
		t.Errorf("SupersededBy = %q, want d1b", d.SupersededBy)
	}
	if n.Allows("") {
		t.Error("a new open approval must not allow")
	}
	if _, err := d.Reraise("d1c", resumed); !errors.Is(err, ErrAlreadyRaised) {
		t.Errorf("raising twice: %v, want ErrAlreadyRaised", err)
	}
}

func TestOnlyASupersededDecisionIsRaisedAgain(t *testing.T) {
	d := newApproval(t)
	if _, err := d.Reraise("x", t0); !errors.Is(err, ErrNotSuperseded) {
		t.Errorf("open decision: %v, want ErrNotSuperseded", err)
	}
	if err := d.Respond(allow(t0.Add(time.Second), "")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Reraise("x", t0); !errors.Is(err, ErrNotSuperseded) {
		t.Errorf("answered decision: %v, want ErrNotSuperseded", err)
	}
	if err := d.Supersede(); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("superseding an answered decision: %v, want ErrDecisionClosed", err)
	}
}

func TestReviewDecisionSurvivesARestart(t *testing.T) {
	d := newPushReview(t, "8e2f1c4")
	if err := d.Supersede(); !errors.Is(err, ErrNotRunBound) {
		t.Fatalf("Supersede = %v, want ErrNotRunBound", err)
	}
	if d.Status != DecisionOpen {
		t.Errorf("status = %s, want open", d.Status)
	}
}

func TestQuestionWithoutDeadlineStaysOpen(t *testing.T) {
	d, err := Raise(NewDecision{ID: "d3", TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Blocking: true, Options: []string{"Allow both", "Allow one"}, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if d.Expire(t0.Add(1000 * time.Hour)) {
		t.Error("a decision without a deadline never expires")
	}
	if err := d.Respond(Response{By: "werner", Option: "Allow one", At: t0.Add(48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if d.Allows("") {
		t.Error("a question never allows anything")
	}
}

// Fail closed, criterion 3: approval is tied to a commit SHA, and a different
// SHA is denied.
func TestApprovalForADifferentSHAIsDenied(t *testing.T) {
	d := newPushReview(t, "aaa111")
	err := d.Respond(allow(t0.Add(time.Minute), "bbb222"))
	if !errors.Is(err, ErrSHAMismatch) {
		t.Fatalf("Respond = %v, want ErrSHAMismatch", err)
	}
	if d.Status != DecisionAnswered || d.Answer != AnswerDeny {
		t.Errorf("a mismatching allow must be recorded as a denial: status %s, answer %q", d.Status, d.Answer)
	}
	for _, sha := range []string{"aaa111", "bbb222", ""} {
		if d.Allows(sha) {
			t.Errorf("Allows(%q) = true after a mismatching approval", sha)
		}
	}
}

func TestApprovalCoversOnlyItsOwnCommit(t *testing.T) {
	d := newPushReview(t, "aaa111")
	if err := d.Respond(allow(t0.Add(time.Minute), "aaa111")); err != nil {
		t.Fatal(err)
	}
	if !d.Allows("aaa111") {
		t.Error("the approved commit must be allowed")
	}
	// The topic gained a commit after the approval: the push must not be allowed.
	if d.Allows("ccc333") {
		t.Error("an approval for an earlier revision must not cover a later one")
	}
	if d.Allows("") {
		t.Error("an approval tied to a commit must not allow an unnamed one")
	}
}

func TestDenialDoesNotNeedTheRightSHA(t *testing.T) {
	d := newPushReview(t, "aaa111")
	if err := d.Respond(Response{By: "werner", Option: AnswerDeny, Reason: "squash first", SHA: "other", At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if d.Answer != AnswerDeny || d.Allows("aaa111") {
		t.Errorf("answer %q, allows %v", d.Answer, d.Allows("aaa111"))
	}
}

func TestOnlyAnAnsweredAllowEverAllows(t *testing.T) {
	// Every status other than answered denies, whatever else is set.
	for _, status := range allDecisionStatuses {
		d := newApproval(t)
		d.Status = status
		d.Answer = AnswerAllow
		want := status == DecisionAnswered
		if got := d.Allows(""); got != want {
			t.Errorf("status %s with answer allow: Allows = %v, want %v", status, got, want)
		}
	}
	d := newApproval(t)
	if err := d.Respond(Response{By: "werner", Option: AnswerDeny, At: t0.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if d.Allows("") {
		t.Error("a denial must not allow")
	}
}

func TestRespondValidation(t *testing.T) {
	d := newApproval(t)
	if err := d.Respond(Response{Option: AnswerAllow, At: t0.Add(time.Second)}); !errors.Is(err, ErrDecisionActor) {
		t.Errorf("no actor: %v, want ErrDecisionActor", err)
	}
	if err := d.Respond(Response{By: "werner", Option: "maybe", At: t0.Add(time.Second)}); !errors.Is(err, ErrDecisionOption) {
		t.Errorf("unknown option: %v, want ErrDecisionOption", err)
	}
	if d.Status != DecisionOpen {
		t.Fatalf("a refused answer must leave the decision open, got %s", d.Status)
	}
	if err := d.Respond(allow(t0.Add(time.Second), "")); err != nil {
		t.Fatal(err)
	}
	if err := d.Respond(Response{By: "werner", Option: AnswerDeny, At: t0.Add(2 * time.Second)}); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("answering twice: %v, want ErrDecisionClosed", err)
	}
	if d.Answer != AnswerAllow {
		t.Error("a second answer must not change the first")
	}
}
