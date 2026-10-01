package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/exitcode"
)

func newApproval(t *testing.T) *Decision {
	t.Helper()
	d, err := raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Blocking: true, Subject: "Bash", Input: "rm -rf build", Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newPushReview(t *testing.T, sha string) *Decision {
	t.Helper()
	d, err := raise(NewDecision{ID: "d2", TaskID: "t1", Kind: DecisionReview, Blocking: true, SHA: sha, Now: t0})
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
	if err := d.respond(Response{By: "werner", Option: AnswerAllow, Reason: "fine", At: t0.Add(time.Minute)}); err != nil {
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
	if d.expire(deadline.Add(-time.Second)) {
		t.Fatal("must not expire before the deadline")
	}
	if !d.expire(deadline) {
		t.Fatal("must expire at the deadline")
	}
	if d.expire(deadline.Add(time.Hour)) {
		t.Error("expiring twice must change nothing")
	}
	if d.Status != DecisionExpired || d.Allows("") {
		t.Errorf("an expired approval must deny: status %s, allows %v", d.Status, d.Allows(""))
	}
	if err := d.respond(allow(deadline.Add(time.Second), "")); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("answering an expired decision: %v, want ErrDecisionClosed", err)
	}
}

func TestLateAnswerNeverCounts(t *testing.T) {
	// The reconciler has not run yet, so the Decision is still open in the
	// store, but the answer arrives after the deadline.
	for _, late := range []time.Duration{0, time.Second, time.Hour} {
		d := newApproval(t)
		err := d.respond(allow(d.Deadline.Add(late), ""))
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
	if err := d.supersede(); err != nil {
		t.Fatal(err)
	}
	if d.Status != DecisionSuperseded || d.Allows("") {
		t.Fatalf("a superseded approval must deny: %s", d.Status)
	}
	if err := d.respond(allow(t0.Add(time.Minute), "")); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("answering a superseded decision: %v, want ErrDecisionClosed", err)
	}

	resumed := t0.Add(2 * time.Minute)
	n, err := d.reraise("d1b", resumed)
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
	if _, err := d.reraise("d1c", resumed); !errors.Is(err, ErrAlreadyRaised) {
		t.Errorf("raising twice: %v, want ErrAlreadyRaised", err)
	}
}

func TestOnlyASupersededDecisionIsRaisedAgain(t *testing.T) {
	d := newApproval(t)
	if _, err := d.reraise("x", t0); !errors.Is(err, ErrNotSuperseded) {
		t.Errorf("open decision: %v, want ErrNotSuperseded", err)
	}
	if err := d.respond(allow(t0.Add(time.Second), "")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.reraise("x", t0); !errors.Is(err, ErrNotSuperseded) {
		t.Errorf("answered decision: %v, want ErrNotSuperseded", err)
	}
	if err := d.supersede(); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("superseding an answered decision: %v, want ErrDecisionClosed", err)
	}
}

func TestReviewDecisionSurvivesARestart(t *testing.T) {
	d := newPushReview(t, "8e2f1c4")
	if err := d.supersede(); !errors.Is(err, ErrNotRunBound) {
		t.Fatalf("Supersede = %v, want ErrNotRunBound", err)
	}
	if d.Status != DecisionOpen {
		t.Errorf("status = %s, want open", d.Status)
	}
}

func TestQuestionWithoutDeadlineStaysOpen(t *testing.T) {
	d, err := raise(NewDecision{ID: "d3", TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Blocking: true, Options: []string{"Allow both", "Allow one"}, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if d.expire(t0.Add(1000 * time.Hour)) {
		t.Error("a decision without a deadline never expires")
	}
	if err := d.respond(Response{By: "werner", Option: "Allow one", At: t0.Add(48 * time.Hour)}); err != nil {
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
	err := d.respond(allow(t0.Add(time.Minute), "bbb222"))
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
	if err := d.respond(allow(t0.Add(time.Minute), "aaa111")); err != nil {
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
	if err := d.respond(Response{By: "werner", Option: AnswerDeny, Reason: "squash first", SHA: "other", At: t0.Add(time.Minute)}); err != nil {
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
	if err := d.respond(Response{By: "werner", Option: AnswerDeny, At: t0.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if d.Allows("") {
		t.Error("a denial must not allow")
	}
}

func TestRespondValidation(t *testing.T) {
	d := newApproval(t)
	if err := d.respond(Response{Option: AnswerAllow, At: t0.Add(time.Second)}); !errors.Is(err, ErrDecisionActor) {
		t.Errorf("no actor: %v, want ErrDecisionActor", err)
	}
	if err := d.respond(Response{By: "werner", Option: "maybe", At: t0.Add(time.Second)}); !errors.Is(err, ErrDecisionOption) {
		t.Errorf("unknown option: %v, want ErrDecisionOption", err)
	}
	if d.Status != DecisionOpen {
		t.Fatalf("a refused answer must leave the decision open, got %s", d.Status)
	}
	if err := d.respond(allow(t0.Add(time.Second), "")); err != nil {
		t.Fatal(err)
	}
	if err := d.respond(Response{By: "werner", Option: AnswerDeny, At: t0.Add(2 * time.Second)}); !errors.Is(err, ErrDecisionClosed) {
		t.Errorf("answering twice: %v, want ErrDecisionClosed", err)
	}
	if d.Answer != AnswerAllow {
		t.Error("a second answer must not change the first")
	}
}

// #49: an answer without a time used to skip the deadline check, so a late
// allow counted and AnsweredAt was year 1.
func TestRespondNeedsTheTimeOfTheAnswer(t *testing.T) {
	d := newApproval(t)
	err := d.respond(Response{By: "werner", Option: AnswerAllow})
	if !errors.Is(err, ErrDecisionTime) {
		t.Fatalf("Respond without a time = %v, want ErrDecisionTime", err)
	}
	if d.Status != DecisionOpen || d.AnsweredAt != nil || d.Answer != "" || d.Allows("") {
		t.Errorf("a refused answer changed the decision: %+v", d)
	}

	at := t0.Add(time.Minute)
	if err := d.respond(Response{By: "werner", Option: AnswerAllow, At: at}); err != nil {
		t.Fatal(err)
	}
	if d.AnsweredAt == nil || !d.AnsweredAt.Equal(at) || d.AnsweredAt.Year() == 1 {
		t.Errorf("AnsweredAt = %v, want %v", d.AnsweredAt, at)
	}
}

func TestZeroTimeNeverReachesTheDeadlineCheck(t *testing.T) {
	// Even when the decision is long past its deadline, an answer without a
	// time is refused rather than counted.
	d := newApproval(t)
	d.Deadline = t0.Add(-time.Hour)
	if err := d.respond(Response{By: "werner", Option: AnswerAllow}); !errors.Is(err, ErrDecisionTime) {
		t.Fatalf("Respond = %v, want ErrDecisionTime", err)
	}
	if d.Allows("") {
		t.Error("an answer without a time must not allow")
	}
}

// #49: re-raising lost the truncation flag, and rebuilt the timeout from
// CreatedAt, which a store may not keep.
func TestReraiseKeepsTruncationAndTheStoredTimeout(t *testing.T) {
	long := strings.Repeat("x", MaxDecisionInput+10)
	d, err := raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Input: long, Timeout: 2 * time.Minute, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if !d.InputTruncated {
		t.Fatal("setup: the input should have been capped")
	}
	if err := d.supersede(); err != nil {
		t.Fatal(err)
	}
	d.CreatedAt = time.Time{} // a store that does not keep it

	resumed := t0.Add(time.Hour)
	n, err := d.reraise("d1b", resumed)
	if err != nil {
		t.Fatal(err)
	}
	if !n.InputTruncated {
		t.Error("the raised-again decision lost InputTruncated")
	}
	if n.Timeout != 2*time.Minute || !n.Deadline.Equal(resumed.Add(2*time.Minute)) {
		t.Errorf("timeout %v, deadline %v; want 2m and %v", n.Timeout, n.Deadline, resumed.Add(2*time.Minute))
	}
}

func TestReraiseOfAQuestionHasNoDeadline(t *testing.T) {
	d, err := raise(NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Blocking: true, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.supersede(); err != nil {
		t.Fatal(err)
	}
	d.CreatedAt = time.Time{}
	n, err := d.reraise("d1b", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !n.Deadline.IsZero() {
		t.Errorf("a question must stay without a deadline, got %v", n.Deadline)
	}
}

// #49: state errors are conflicts (exit code 5) and still match their sentinels.
func TestDecisionStateErrorsAreConflicts(t *testing.T) {
	answered := newApproval(t)
	if err := answered.respond(allow(t0.Add(time.Second), "")); err != nil {
		t.Fatal(err)
	}
	late := newApproval(t)
	mismatch := newPushReview(t, "aaa111")
	open := newApproval(t)
	raised := newApproval(t)
	_ = raised.supersede()
	_, _ = raised.reraise("d1b", t0.Add(time.Minute))

	tests := []struct {
		name string
		err  error
		want error
	}{
		{"answering a closed decision", answered.respond(allow(t0.Add(2*time.Second), "")), ErrDecisionClosed},
		{"answering after the deadline", late.respond(allow(late.Deadline, "")), ErrDecisionExpired},
		{"allow for another commit", mismatch.respond(allow(t0.Add(time.Second), "bbb222")), ErrSHAMismatch},
		{"superseding a review decision", newPushReview(t, "aaa111").supersede(), ErrNotRunBound},
		{"raising an open decision again", func() error { _, err := open.reraise("x", t0); return err }(), ErrNotSuperseded},
		{"raising twice", func() error { _, err := raised.reraise("y", t0); return err }(), ErrAlreadyRaised},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.want) {
				t.Fatalf("error = %v, want it to match %v", tc.err, tc.want)
			}
			if got := exitcode.From(tc.err); got != exitcode.Conflict {
				t.Errorf("exit code = %d, want Conflict (%d)", got, exitcode.Conflict)
			}
		})
	}
}

// #57: an approval that lost its deadline (a bad load, a hand-edited row) must
// not let a late allow through. "Zero means none" holds for a question only.
func TestApprovalWithoutDeadlineFailsClosed(t *testing.T) {
	t.Run("a late allow is refused and the approval expires", func(t *testing.T) {
		d := newApproval(t)
		d.Deadline = time.Time{}
		err := d.respond(allow(t0.Add(24*time.Hour), ""))
		if !errors.Is(err, ErrDecisionExpired) {
			t.Fatalf("Respond = %v, want ErrDecisionExpired", err)
		}
		if d.Status != DecisionExpired || d.Allows("") {
			t.Errorf("status = %s, Allows = %v; want expired and false", d.Status, d.Allows(""))
		}
	})
	t.Run("Expire expires it", func(t *testing.T) {
		d := newApproval(t)
		d.Deadline = time.Time{}
		if !d.expire(t0) || d.Status != DecisionExpired {
			t.Errorf("Expire = false or status = %s", d.Status)
		}
	})
	t.Run("an answered allow without a deadline does not allow", func(t *testing.T) {
		d := newApproval(t)
		if err := d.respond(allow(t0.Add(time.Second), "")); err != nil {
			t.Fatal(err)
		}
		d.Deadline = time.Time{} // lost after the answer was stored
		if d.Allows("") {
			t.Error("an approval without a deadline must not allow")
		}
	})
	t.Run("a question still waits without a deadline", func(t *testing.T) {
		q, err := raise(NewDecision{ID: "q1", TaskID: "t1", RunID: "r1", Kind: DecisionQuestion, Blocking: true, Now: t0})
		if err != nil {
			t.Fatal(err)
		}
		if err := q.respond(Response{By: "werner", Option: "yes", At: t0.Add(24 * time.Hour)}); err != nil {
			t.Errorf("Respond to a question without a deadline = %v", err)
		}
	})
	t.Run("a review decision is not an approval", func(t *testing.T) {
		d := newPushReview(t, "aaa111")
		if err := d.respond(allow(t0.Add(24*time.Hour), "aaa111")); err != nil {
			t.Errorf("Respond to a review = %v", err)
		}
		if !d.Allows("aaa111") {
			t.Error("an answered review allow must allow its commit")
		}
	})
}

// #57: validation errors are usage errors (exit code 2), and still match
// their sentinels.
func TestDecisionValidationErrorsAreUsage(t *testing.T) {
	respond := func(r Response) error { return newApproval(t).respond(r) }
	raise := func(mod func(*NewDecision)) error {
		spec := NewDecision{ID: "d1", TaskID: "t1", RunID: "r1", Kind: DecisionApproval, Now: t0}
		mod(&spec)
		_, err := raise(spec)
		return err
	}
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"an answer without a time", respond(Response{By: "w", Option: AnswerAllow}), ErrDecisionTime},
		{"an answer without an actor", respond(Response{Option: AnswerAllow, At: t0}), ErrDecisionActor},
		{"an option that is not offered", respond(Response{By: "w", Option: "maybe", At: t0}), ErrDecisionOption},
		{"raise without an ID", raise(func(n *NewDecision) { n.ID = "" }), ErrDecisionID},
		{"raise with an unknown kind", raise(func(n *NewDecision) { n.Kind = "poll" }), ErrDecisionKind},
		{"raise an approval without a run", raise(func(n *NewDecision) { n.RunID = "" }), ErrDecisionRun},
		{"raise a review without a SHA", raise(func(n *NewDecision) { n.Kind, n.RunID = DecisionReview, "" }), ErrDecisionSHA},
		{"raise with a negative timeout", raise(func(n *NewDecision) { n.Timeout = -1 }), ErrDecisionTimeout},
		{"raise without a time", raise(func(n *NewDecision) { n.Now = time.Time{} }), ErrDecisionTime},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.want) {
				t.Fatalf("error = %v, want it to match %v", tc.err, tc.want)
			}
			if got := exitcode.From(tc.err); got != exitcode.Usage {
				t.Errorf("exit code = %d, want Usage (%d)", got, exitcode.Usage)
			}
		})
	}
}
