package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// openApproval waits until an approval Decision is open and returns it.
func (r *rig) openApproval() domain.Decision {
	r.t.Helper()
	for range 500 {
		in, err := r.svc.Inbox(bg)
		must(r.t, err)
		for _, d := range in {
			if d.Kind == domain.DecisionApproval {
				return d
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatal("no approval Decision was raised")
	return domain.Decision{}
}

type approved struct {
	a   agent.Approval
	err error
}

// ask starts the agent's permission prompt in a goroutine. The goroutine is
// ended with the test: a test that returns early (a failed wait) cancels the
// prompt and waits for it, so it never touches the service after the test is
// over (issue #260).
func (r *rig) ask(ctx context.Context, input string) <-chan approved {
	out := make(chan approved, 1)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.t.Cleanup(func() { cancel(); <-done }) // runs before the service's Shutdown, which newRig registered first
	go func() {
		defer close(done)
		a, err := r.svc.approverFor("t1", "r1").Approve(ctx, agent.ApprovalRequest{ID: "req-1", Tool: "Bash", Input: input})
		out <- approved{a, err}
	}()
	return out
}

// A permission prompt becomes a blocking approval Decision with the tool and a
// capped input, the task waits for guidance, and the human's answer is what the
// agent is told (design §4.2, D26).
func TestAPermissionPromptBecomesAnApprovalDecisionAndTheAnswerGoesBack(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		option, reason string
		allow          bool
	}{
		{domain.AnswerAllow, "", true},
		{domain.AnswerDeny, "not in this repository", false},
	} {
		t.Run(tc.option, func(t *testing.T) {
			r := newRig(t)
			r.live()
			res := r.ask(bg, strings.Repeat("x", 3*domain.MaxDecisionInput))
			d := r.openApproval()
			if d.Subject != "Bash" || !d.Blocking || len([]rune(d.Input)) > domain.MaxDecisionInput || d.RunID != "r1" || d.Deadline.IsZero() {
				t.Fatalf("decision %+v", d)
			}
			if got := r.load().Task().State; got != domain.TaskAwaitingGuidance {
				t.Errorf("task %s while the agent waits, want awaiting_guidance", got)
			}
			must(t, r.svc.AnswerDecision(bg, d.ID, domain.Response{By: "werner", Option: tc.option, Reason: tc.reason, At: r.clock.now}))
			got := <-res
			if got.err != nil || got.a.Allow != tc.allow || got.a.Reason != tc.reason {
				t.Errorf("the agent was told %+v (%v), want allow=%v reason %q", got.a, got.err, tc.allow, tc.reason)
			}
			if st := r.load().Task().State; st != domain.TaskRunning {
				t.Errorf("task %s after the answer, want running", st)
			}
		})
	}
}

// An answer for an approval nobody is waiting for is refused and changes nothing
// (D23): the agent's process is gone, so the answer could only reach nothing, or
// the wrong request.
func TestAnAnswerForAnApprovalNobodyWaitsForIsRefused(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	a := r.load()
	_, err := a.RaiseDecision(domain.NewDecision{ID: "ap-old", RunID: "r1", Kind: domain.DecisionApproval, Blocking: true, Subject: "Bash", Now: r.clock.now})
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)

	err = r.svc.AnswerDecision(bg, "ap-old", domain.Response{By: "w", Option: domain.AnswerAllow, At: r.clock.now})
	var ce *domain.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if d, _ := r.load().Decision("ap-old"); d.Status != domain.DecisionOpen {
		t.Errorf("the refused answer changed the decision: %s", d.Status)
	}
	// ... and so is an answer to an ID that does not exist at all
	if err := r.svc.AnswerDecision(bg, "no-such", domain.Response{By: "w", Option: domain.AnswerAllow, At: r.clock.now}); err == nil {
		t.Error("an unknown decision was answered")
	}
}

// No answer in time: the agent is told no, and the Decision is expired, so it
// does not stay open for an agent that no longer waits.
func TestAnUnansweredApprovalExpiresAndDenies(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	ctx, cancel := context.WithTimeout(bg, time.Second) // long enough that a loaded machine raises the Decision first
	defer cancel()
	res := r.ask(ctx, "make deploy")
	d := r.openApproval()
	got := <-res
	if got.err == nil || got.a.Allow {
		t.Fatalf("an unanswered prompt returned %+v, %v, want an error and no allow", got.a, got.err)
	}
	after, _ := r.load().Decision(d.ID)
	if after.Status != domain.DecisionExpired {
		t.Errorf("decision %s, want expired", after.Status)
	}
	if st := r.load().Task().State; st != domain.TaskRunning {
		t.Errorf("task %s, want running: the expired approval frees it", st)
	}
	// a late answer is now refused
	if err := r.svc.AnswerDecision(bg, d.ID, domain.Response{By: "w", Option: domain.AnswerAllow, At: r.clock.now}); err == nil {
		t.Error("a late answer was accepted")
	}
}

// A pause supersedes the open approval; the stopped session's Approver gives up
// and nothing later answers it.
func TestPausingARunSupersedesItsApproval(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	ctx, cancel := context.WithCancel(bg)
	res := r.ask(ctx, "ls")
	d := r.openApproval()

	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error {
		_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "q1", r.clock.now)
		return err
	}))
	cancel() // the session is stopped with its run
	if got := <-res; got.err == nil || got.a.Allow {
		t.Fatalf("the agent was told %+v, %v", got.a, got.err)
	}
	if after, _ := r.load().Decision(d.ID); after.Status != domain.DecisionSuperseded {
		t.Errorf("decision %s, want superseded", after.Status)
	}
	if err := r.svc.AnswerDecision(bg, d.ID, domain.Response{By: "w", Option: domain.AnswerAllow, At: r.clock.now}); err == nil {
		t.Error("an answer to a superseded approval was accepted")
	}
}

// Through the service's own launch: the spec has no approver, the service gives
// it one, and the agent's prompt reaches the inbox.
func TestTheServiceGivesAManualSpecItsApprover(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.svc.cfg.Spec = func(domain.Task, domain.Run) agent.StartSpec {
		return agent.StartSpec{EnvID: "env", Workdir: "/work", Prompt: "continue", Auth: agent.AuthSubscription, PermissionMode: agent.PermissionManual, ApprovalTimeout: 5 * time.Second}
	}
	must(t, r.rt.Restart(bg))
	r.agent.AskApproval("Bash", "make test")
	r.reconcile()
	d := r.openApproval()
	if d.Subject != "Bash" || d.Input != "make test" {
		t.Fatalf("decision %+v", d)
	}
	must(t, r.svc.AnswerDecision(bg, d.ID, domain.Response{By: "w", Option: domain.AnswerAllow, At: r.clock.now}))
	r.svc.Wait()
	if after, _ := r.load().Decision(d.ID); after.Status != domain.DecisionAnswered || !after.Allows("") {
		t.Errorf("decision %+v", after)
	}
}
