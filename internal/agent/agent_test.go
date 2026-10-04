package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
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

	// The session is stopped while the approver answers allow: the answer is
	// ready before Ask's own context has seen the cancel (it reaches that a
	// moment later, through the parent's Done), so an Ask that only selects
	// honours the answer. The race is real, so 200 runs: an unfixed Ask passes
	// them all only with a vanishing chance.
	for i := 0; i < 200; i++ {
		parent := newStopCtx()
		stopThenAllow := approver(func(context.Context, ApprovalRequest) (Approval, error) {
			parent.stop()
			return Approval{Allow: true}, nil
		})
		if got := Ask(parent, stopThenAllow, time.Second, ApprovalRequest{}); got.Allow || got.Reason == "" {
			t.Fatalf("run %d: an approval given after the cancel must deny with a reason, got %+v", i, got)
		}
	}
	// A context cancelled before the call, an approver that waits for it.
	block := approver(func(ctx context.Context, _ ApprovalRequest) (Approval, error) {
		<-ctx.Done()
		return Approval{Allow: true}, nil
	})
	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if got := Ask(ctx, block, time.Second, ApprovalRequest{}); got.Allow || got.Reason == "" {
			t.Fatalf("run %d: a cancelled context must deny with a reason, got %+v", i, got)
		}
		if got := Ask(context.Background(), block, time.Millisecond, ApprovalRequest{}); got.Allow || got.Reason == "" {
			t.Fatalf("run %d: a timeout must deny with a reason, got %+v", i, got)
		}
	}
	// An answer with a live context is still honoured.
	if got := Ask(context.Background(), approver(func(context.Context, ApprovalRequest) (Approval, error) {
		return Approval{Allow: true}, nil
	}), time.Second, ApprovalRequest{}); !got.Allow {
		t.Errorf("an answer with a live context must be honoured, got %+v", got)
	}
	if got := Ask(context.Background(), nil, time.Second, ApprovalRequest{}); got.Allow || got.Reason == "" {
		t.Errorf("no approver must deny with a reason, got %+v", got)
	}
}

func TestAskCapsTheInput(t *testing.T) {
	var seen string
	var cut bool
	ap := approver(func(_ context.Context, req ApprovalRequest) (Approval, error) {
		seen, cut = req.Input, req.Truncated
		return Approval{}, nil
	})
	Ask(context.Background(), ap, time.Second, ApprovalRequest{Input: strings.Repeat("é", domain.MaxDecisionInput+50)})
	if got := len([]rune(seen)); got != domain.MaxDecisionInput || !cut {
		t.Errorf("the approver saw %d characters, truncated %v; want %d, true", got, cut, domain.MaxDecisionInput)
	}
	Ask(context.Background(), ap, time.Second, ApprovalRequest{Input: "short"})
	if seen != "short" || cut {
		t.Errorf("a short input changed: %q, truncated %v", seen, cut)
	}
}

func TestCheckSpec(t *testing.T) {
	full := Capabilities{Headless: true, StructuredEvents: true, MidRunInstruction: true, HostApprovals: true, AuthModes: []AuthMode{AuthSubscription}}
	degraded := Capabilities{Headless: true, StructuredEvents: true, AuthModes: []AuthMode{AuthSubscription}}
	ap := ApproverFunc(func(context.Context, ApprovalRequest) (Approval, error) { return Approval{}, nil })

	tests := []struct {
		name string
		caps Capabilities
		spec StartSpec
		want error
	}{
		{"manual with an approver", full, StartSpec{Auth: AuthSubscription, Approver: ap}, nil},
		{"the empty mode is manual", full, StartSpec{Auth: AuthSubscription, PermissionMode: PermissionManual, Approver: ap}, nil},
		{"manual without an approver", full, StartSpec{Auth: AuthSubscription}, ErrNoApprover},
		{"manual on an agent without host approvals", degraded, StartSpec{Auth: AuthSubscription, Approver: ap}, ErrUnsupported},
		{"dontAsk needs no approver", degraded, StartSpec{Auth: AuthSubscription, PermissionMode: PermissionDontAsk, AllowedTools: []string{"Read"}}, nil},
		{"dontAsk on a full agent", full, StartSpec{Auth: AuthSubscription, PermissionMode: PermissionDontAsk}, nil},
		{"an allowlist with manual", full, StartSpec{Auth: AuthSubscription, Approver: ap, AllowedTools: []string{"Read"}}, ErrBadSpec},
		{"bypassPermissions is not offered", full, StartSpec{Auth: AuthSubscription, PermissionMode: "bypassPermissions", Approver: ap}, ErrUnsupported},
		{"auto is not offered", full, StartSpec{Auth: AuthSubscription, PermissionMode: "auto", Approver: ap}, ErrUnsupported},
		{"an auth mode the agent lacks", full, StartSpec{Auth: AuthAPIKey, Approver: ap}, ErrUnsupportedAuth},
	}
	for _, tc := range tests {
		if err := tc.caps.CheckSpec(tc.spec); !errors.Is(err, tc.want) {
			t.Errorf("%s: CheckSpec = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// stopCtx is a context that is not a stdlib one, so a context derived from it
// learns of the cancel through a goroutine, a moment after stop returns.
type stopCtx struct {
	once sync.Once
	done chan struct{}
}

func newStopCtx() *stopCtx { return &stopCtx{done: make(chan struct{})} }

func (c *stopCtx) stop()                       { c.once.Do(func() { close(c.done) }) }
func (c *stopCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *stopCtx) Done() <-chan struct{}       { return c.done }
func (c *stopCtx) Value(any) any               { return nil }
func (c *stopCtx) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func TestAskDeniesAPanickingApprover(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	const leak = "tok-SECRET\x1b[31m"
	ap := ApproverFunc(func(context.Context, ApprovalRequest) (Approval, error) {
		panic(leak)
	})
	got := Ask(context.Background(), ap, time.Second, ApprovalRequest{ID: "1", Tool: "Bash"})
	if got.Allow || got.Reason != "approval failed: denied" {
		t.Fatalf("got %+v, want the fixed denial", got)
	}
	if strings.Contains(got.Reason, "SECRET") || strings.ContainsRune(got.Reason, '\x1b') {
		t.Fatalf("reason leaks the panic value: %q", got.Reason)
	}
	if buf.Len() == 0 {
		t.Fatal("the panic was not logged")
	}
	if strings.Contains(buf.String(), "SECRET") {
		t.Fatalf("log leaks the panic value: %q", buf.String())
	}
}

type ptrApprover struct{}

func (*ptrApprover) Approve(context.Context, ApprovalRequest) (Approval, error) {
	return Approval{Allow: true}, nil
}

func TestAskDeniesATypedNilApprover(t *testing.T) {
	var f ApproverFunc
	var p *ptrApprover
	tests := []struct {
		name string
		ap   Approver
	}{
		{"nil interface", nil},
		{"nil func", f},
		{"nil pointer", p},
	}
	for _, tc := range tests {
		got := Ask(context.Background(), tc.ap, time.Second, ApprovalRequest{ID: "1"})
		if got.Allow || got.Reason != "no approver: denied" {
			t.Errorf("%s: got %+v, want the no-approver denial", tc.name, got)
		}
	}
}
