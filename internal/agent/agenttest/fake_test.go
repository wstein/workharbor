package agenttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
)

func allowAll() agent.Approver {
	return agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) {
		return agent.Approval{Allow: true}, nil
	})
}

func spec() agent.StartSpec {
	return agent.StartSpec{EnvID: "e1", Workdir: "/work", Prompt: "fix it", Auth: agent.AuthSubscription, Approver: allowAll(), ApprovalTimeout: time.Second}
}

// dontAskSpec is the spec for an agent without host approvals.
func dontAskSpec() agent.StartSpec {
	s := spec()
	s.PermissionMode, s.AllowedTools = agent.PermissionDontAsk, []string{"Read"}
	return s
}

func drain(s agent.Session) []agent.Event {
	var events []agent.Event
	for e := range s.Events() {
		events = append(events, e)
	}
	return events
}

func TestUnarrangedSessionFinishesWithDone(t *testing.T) {
	f := NewFake(FullCaps())
	s, err := f.Start(context.Background(), spec())
	if err != nil {
		t.Fatal(err)
	}
	events := drain(s)
	res, err := s.Wait()
	if err != nil || res.Status != agent.ResultCompleted || res.Text != "done" || res.SessionID == "" {
		t.Errorf("result = %+v, %v", res, err)
	}
	if len(events) < 2 || events[0].Kind != agent.EventSession || !hasText(events, agent.EventMessage, "done") {
		t.Errorf("events = %+v", events)
	}
}

func TestStartChecksAuthAndApprover(t *testing.T) {
	f := NewFake(DegradedCaps())
	bad := spec()
	bad.Auth = agent.AuthAPIKey
	if _, err := f.Start(context.Background(), bad); !errors.Is(err, agent.ErrUnsupportedAuth) {
		t.Errorf("an auth mode the agent does not offer = %v, want ErrUnsupportedAuth", err)
	}
	full := NewFake(FullCaps())
	noApprover := spec()
	noApprover.Approver = nil
	if _, err := full.Start(context.Background(), noApprover); !errors.Is(err, agent.ErrNoApprover) {
		t.Errorf("no approver for an agent that routes approvals = %v, want ErrNoApprover", err)
	}
}

func TestBlockedSessionTakesInstructionsAndStops(t *testing.T) {
	ctx := context.Background()
	f := NewFake(FullCaps())
	f.Block()
	s, err := f.Start(ctx, spec())
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Instruct(ctx, "use the helper")
	if err != nil || d != agent.DeliveryNextTurn {
		t.Fatalf("Instruct = %q, %v; want next_turn", d, err)
	}
	_ = s.Stop(ctx)
	_ = s.Stop(ctx) // stopping twice is harmless
	events := drain(s)
	res, _ := s.Wait()
	if res.Status != agent.ResultStopped || res.SessionID == "" {
		t.Errorf("result = %+v, want stopped with a session ID", res)
	}
	var heard bool
	for _, e := range events {
		heard = heard || e.Text == "user: use the helper"
	}
	if !heard {
		t.Error("the instruction never reached the session")
	}
	if _, err := s.Instruct(ctx, "too late"); !errors.Is(err, agent.ErrNotRunning) {
		t.Errorf("Instruct after the end = %v, want ErrNotRunning", err)
	}
}

func TestDegradedAgentTurnsAMessageIntoAResumedTurn(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DegradedCaps())
	f.Block()
	s, _ := f.Start(ctx, dontAskSpec())
	if d, err := s.Instruct(ctx, "hello"); err != nil || d != agent.DeliveryResumedTurn {
		t.Errorf("Instruct = %q, %v; want resumed_turn", d, err)
	}
	_ = s.Stop(ctx)
}

func TestResume(t *testing.T) {
	ctx := context.Background()
	f := NewFake(FullCaps())
	f.Block()
	s, _ := f.Start(ctx, spec())
	_ = s.Stop(ctx)
	res, _ := s.Wait()

	f.Finish("resumed")
	r, err := f.Resume(ctx, spec(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Wait(); got.SessionID != res.SessionID || got.Text != "resumed" {
		t.Errorf("resumed result = %+v, want the same session", got)
	}
	if _, err := f.Resume(ctx, spec(), "no-such-session"); !errors.Is(err, agent.ErrNoSession) {
		t.Errorf("unknown session = %v, want ErrNoSession", err)
	}
	noResume := FullCaps()
	noResume.SessionResume = false
	if _, err := NewFake(noResume).Resume(ctx, spec(), "x"); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("resume without the capability = %v, want ErrUnsupported", err)
	}
}

func TestAuthAndQuotaEndTheRunWithoutAnError(t *testing.T) {
	ctx := context.Background()
	f := NewFake(FullCaps())
	f.AuthExpires()
	s, _ := f.Start(ctx, spec())
	drain(s)
	if res, err := s.Wait(); err != nil || res.Status != agent.ResultAuthExpired {
		t.Errorf("auth: %+v, %v", res, err)
	}

	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	f.QuotaExhausted(reset)
	s, _ = f.Start(ctx, spec())
	drain(s)
	if res, err := s.Wait(); err != nil || res.Status != agent.ResultQuotaExhausted || !res.ResetAt.Equal(reset) {
		t.Errorf("quota: %+v, %v", res, err)
	}

	degraded := NewFake(DegradedCaps())
	degraded.QuotaExhausted(reset)
	s, _ = degraded.Start(ctx, dontAskSpec())
	drain(s)
	if res, _ := s.Wait(); !res.ResetAt.IsZero() {
		t.Errorf("an agent that does not report quota gave a reset time: %v", res.ResetAt)
	}
}

func TestOnlyACooperativeAgentGivesAPauser(t *testing.T) {
	ctx := context.Background()
	plain := NewFake(FullCaps())
	plain.Block()
	s, _ := plain.Start(ctx, spec())
	if _, ok := s.(agent.Pauser); ok {
		t.Error("an agent without cooperative pause must not return a Pauser")
	}
	_ = s.Stop(ctx)

	caps := FullCaps()
	caps.CooperativePause = true
	coop := NewFake(caps)
	coop.Block()
	s, _ = coop.Start(ctx, spec())
	if _, ok := s.(agent.Pauser); !ok {
		t.Error("an agent with cooperative pause must return a Pauser")
	}
	_ = s.Stop(ctx)
}
