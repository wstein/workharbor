package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

func TestModeFollowsTheCapabilities(t *testing.T) {
	tests := []struct {
		name string
		caps Capabilities
		want Mode
	}{
		{"claude code today", Capabilities{Headless: true, StructuredEvents: true, MidRunInstruction: true, HostApprovals: true}, ModeFull},
		{"events and injection but no host approvals", Capabilities{Headless: true, StructuredEvents: true, MidRunInstruction: true}, ModeDegraded},
		{"events and host approvals but no injection", Capabilities{Headless: true, StructuredEvents: true, HostApprovals: true}, ModeDegraded},
		{"headless with events only (codex exec)", Capabilities{Headless: true, StructuredEvents: true}, ModeDegraded},
		{"no structured events", Capabilities{Headless: true, MidRunInstruction: true, HostApprovals: true}, ModeUnsupported},
		{"not headless", Capabilities{StructuredEvents: true, MidRunInstruction: true, HostApprovals: true}, ModeUnsupported},
		{"nothing", Capabilities{}, ModeUnsupported},
	}
	for _, tc := range tests {
		if got := tc.caps.Mode(); got != tc.want {
			t.Errorf("%s: Mode = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestCooperativePauseDoesNotMakeAnAgentFull(t *testing.T) {
	// D11: cooperative pause is a flag and not a requirement of either mode.
	c := Capabilities{Headless: true, StructuredEvents: true, MidRunInstruction: true, HostApprovals: true, CooperativePause: false}
	if c.Mode() != ModeFull {
		t.Error("an agent without cooperative pause can still be full")
	}
	c.CooperativePause = true
	if c.Mode() != ModeFull {
		t.Error("cooperative pause must not change the mode")
	}
}

func TestSupports(t *testing.T) {
	c := Capabilities{AuthModes: []AuthMode{AuthSubscription}}
	if !c.Supports(AuthSubscription) || c.Supports(AuthAPIKey) || (Capabilities{}).Supports(AuthSubscription) {
		t.Errorf("Supports gave the wrong answers for %v", c.AuthModes)
	}
}

func TestDefaultApprovalTimeoutMatchesTheDecisionDefault(t *testing.T) {
	if DefaultApprovalTimeout != domain.DefaultApprovalTimeout {
		t.Errorf("agent default %v differs from the decision default %v", DefaultApprovalTimeout, domain.DefaultApprovalTimeout)
	}
}

func approver(f func(ctx context.Context, req ApprovalRequest) (Approval, error)) Approver {
	return ApproverFunc(f)
}

func TestAskReturnsTheHumansAnswer(t *testing.T) {
	allow := approver(func(context.Context, ApprovalRequest) (Approval, error) {
		return Approval{Allow: true, Reason: "fine"}, nil
	})
	if got := Ask(context.Background(), allow, time.Second, ApprovalRequest{Tool: "Bash"}); !got.Allow || got.Reason != "fine" {
		t.Errorf("Ask = %+v, want the allow", got)
	}
	deny := approver(func(context.Context, ApprovalRequest) (Approval, error) { return Approval{Reason: "not that"}, nil })
	if got := Ask(context.Background(), deny, time.Second, ApprovalRequest{Tool: "Bash"}); got.Allow || got.Reason != "not that" {
		t.Errorf("Ask = %+v, want the denial with its reason", got)
	}
}

// Fail closed: every way the answer can go missing is a denial the agent sees.
func TestAskFailsClosed(t *testing.T) {
	boom := approver(func(context.Context, ApprovalRequest) (Approval, error) {
		return Approval{Allow: true}, errors.New("supervisor unreachable")
	})
	if got := Ask(context.Background(), boom, time.Second, ApprovalRequest{}); got.Allow || got.Reason == "" {
		t.Errorf("an error must deny with a reason, got %+v", got)
	}

	// An approver that ignores its context and answers allow far too late.
	slow := approver(func(context.Context, ApprovalRequest) (Approval, error) {
		time.Sleep(300 * time.Millisecond)
		return Approval{Allow: true}, nil
	})
	start := time.Now()
	got := Ask(context.Background(), slow, 30*time.Millisecond, ApprovalRequest{})
	if got.Allow || got.Reason == "" {
		t.Errorf("a late answer must deny with a reason, got %+v", got)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Errorf("Ask waited %v for an approver that ignored the timeout", time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	block := approver(func(ctx context.Context, _ ApprovalRequest) (Approval, error) {
		<-ctx.Done()
		return Approval{Allow: true}, nil
	})
	if got := Ask(ctx, block, time.Second, ApprovalRequest{}); got.Allow {
		t.Errorf("a cancelled context must deny, got %+v", got)
	}
	if got := Ask(context.Background(), nil, time.Second, ApprovalRequest{}); got.Allow || got.Reason == "" {
		t.Errorf("no approver must deny with a reason, got %+v", got)
	}
}

func TestAskCapsTheInput(t *testing.T) {
	var seen string
	ap := approver(func(_ context.Context, req ApprovalRequest) (Approval, error) {
		seen = req.Input
		return Approval{}, nil
	})
	Ask(context.Background(), ap, time.Second, ApprovalRequest{Input: strings.Repeat("é", domain.MaxDecisionInput+50)})
	if got := len([]rune(seen)); got != domain.MaxDecisionInput {
		t.Errorf("the approver saw %d characters, want %d", got, domain.MaxDecisionInput)
	}
	Ask(context.Background(), ap, time.Second, ApprovalRequest{Input: "short"})
	if seen != "short" {
		t.Errorf("a short input changed: %q", seen)
	}
}
