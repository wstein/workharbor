package agenttest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// Scenarios lets the suite arrange what the next session does. The fake
// implements it directly; a real adapter implements it by pointing the agent
// at a scripted stand-in (a recorded transcript, a stub binary).
type Scenarios interface {
	Finish(text string)
	AskApproval(tool, input string)
	Block()
	AuthExpires()
	QuotaExhausted(resetAt time.Time)
}

// Harness is what an adapter gives the suite.
type Harness struct {
	Adapter   agent.Adapter
	Scenarios Scenarios
	// Spec returns a valid StartSpec: an auth mode the adapter offers and a
	// working directory. The suite sets the approver itself.
	Spec func() agent.StartSpec
}

// ErrSkip is returned by a check that does not apply to the adapter.
var ErrSkip = errors.New("skipped: the adapter does not claim the capability this check covers")

// Check is one conformance requirement. It returns nil when the adapter meets
// it, so tests can also show that a defective adapter fails it.
type Check struct {
	Name string
	Fn   func(ctx context.Context, h Harness) error
}

// Checks returns the conformance requirements of design §5.2.
func Checks() []Check {
	return []Check{
		{"capabilities are reported", checkCapabilities},
		{"start checks the auth mode and the approver", checkStart},
		{"the session ID arrives with the session event", checkSessionEvent},
		{"events are typed and the session ends with a result", checkEvents},
		{"an allowed approval lets the agent go on", checkApprovalAllowed},
		{"a denial reaches the agent with its reason", checkApprovalDenied},
		{"an approver error denies", checkApprovalError},
		{"an approver that does not answer in time denies", checkApprovalTimeout},
		{"stop cancels a pending approval", checkStopCancelsApproval},
		{"the input of an approval is capped", checkApprovalCap},
		{"instruction delivery is honest", checkInstruct},
		{"stop is a hard interrupt and the session is resumable", checkStopAndResume},
		{"auth expiry ends the run without failing it", checkAuthExpired},
		{"quota exhaustion ends the run without failing it", checkQuota},
		{"cooperative pause is a capability flag", checkPause},
	}
}

// Run runs the suite against an adapter. newHarness is called once per check
// with a fresh adapter.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()
	for _, c := range Checks() {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.Fn(ctx, newHarness(t)); errors.Is(err, ErrSkip) {
				t.Skip(err)
			} else if err != nil {
				t.Error(err)
			}
		})
	}
}

// Failures runs every check and returns the ones that failed, by name.
func Failures(ctx context.Context, newHarness func() Harness) map[string]error {
	failed := map[string]error{}
	for _, c := range Checks() {
		if err := c.Fn(ctx, newHarness()); err != nil && !errors.Is(err, ErrSkip) {
			failed[c.Name] = err
		}
	}
	return failed
}

func show(err error) string {
	if err == nil {
		return "no error"
	}
	return err.Error()
}

var knownKinds = map[agent.EventKind]bool{
	agent.EventSession: true, agent.EventMessage: true, agent.EventToolCall: true, agent.EventToolResult: true,
	agent.EventDiff: true, agent.EventTestResult: true, agent.EventUsage: true, agent.EventApproval: true,
	agent.EventAuthExpired: true, agent.EventQuotaExhausted: true, agent.EventError: true,
}

// collect reads a session's events until it ends or the time runs out.
func collect(s agent.Session, limit time.Duration) ([]agent.Event, error) {
	var events []agent.Event
	timeout := time.After(limit)
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return events, nil
			}
			events = append(events, e)
		case <-timeout:
			return events, errors.New("the session did not end in time")
		}
	}
}

// SessionWait is how long the suite waits for a session event. A test of the
// suite itself shortens it.
var SessionWait = 2 * time.Second

// awaitSession reads events until the session event, which says the session
// ID, and returns the events read, that one last. Instruct and Stop come after
// it: Claude Code reports the ID only after the first message (spike #1).
func awaitSession(s agent.Session, limit time.Duration) ([]agent.Event, error) {
	var events []agent.Event
	timeout := time.After(limit)
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return events, errors.New("the session ended without a session event")
			}
			events = append(events, e)
			if e.Kind == agent.EventSession {
				if e.SessionID == "" || s.ID() != e.SessionID {
					return events, fmt.Errorf("the session event says %q and ID() says %q, want the same non-empty ID", e.SessionID, s.ID())
				}
				return events, nil
			}
		case <-timeout:
			return events, errors.New("the session never reported its ID (no session event)")
		}
	}
}

// startAndWait starts a session and waits for its session event.
func startAndWait(ctx context.Context, h Harness, spec agent.StartSpec) (agent.Session, error) {
	s, err := h.Adapter.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	if _, err := awaitSession(s, SessionWait); err != nil {
		_ = s.Stop(ctx)
		return nil, err
	}
	return s, nil
}

func checkSessionEvent(ctx context.Context, h Harness) error {
	h.Scenarios.Finish("hello")
	s, err := startAndWait(ctx, h, newSpec(h))
	if err != nil {
		return err
	}
	id := s.ID()
	if _, err := collect(s, 5*time.Second); err != nil {
		return err
	}
	if res, err := s.Wait(); err != nil || res.SessionID != id {
		return fmt.Errorf("the result's session ID is %q (%s), want the one the session event gave: %q", res.SessionID, show(err), id)
	}
	return nil
}

func hasText(events []agent.Event, kind agent.EventKind, sub string) bool {
	for _, e := range events {
		if e.Kind == kind && strings.Contains(e.Text, sub) {
			return true
		}
	}
	return false
}

// approvalOf returns the record of the approval event, or an error if there is
// not exactly one with a request ID. The suite asserts on this record and not
// on text the agent prints.
func approvalOf(events []agent.Event) (agent.ApprovalRecord, error) {
	var found []agent.ApprovalRecord
	for _, e := range events {
		if e.Kind == agent.EventApproval {
			if e.Approval == nil {
				return agent.ApprovalRecord{}, fmt.Errorf("an approval event carries no record: %+v", e)
			}
			found = append(found, *e.Approval)
		}
	}
	if len(found) != 1 {
		return agent.ApprovalRecord{}, fmt.Errorf("want one approval event, got %d: %+v", len(found), events)
	}
	if found[0].ID == "" {
		return found[0], errors.New("the approval event has no request ID")
	}
	return found[0], nil
}

func hasKind(events []agent.Event, kind agent.EventKind) bool {
	return hasText(events, kind, "")
}

func checkCapabilities(_ context.Context, h Harness) error {
	c := h.Adapter.Capabilities()
	if h.Adapter.Name() == "" {
		return errors.New("an adapter must have a name")
	}
	if c.ContractVersion != agent.ContractVersion {
		return fmt.Errorf("the adapter implements contract version %d, this is version %d", c.ContractVersion, agent.ContractVersion)
	}
	if !c.StructuredEvents {
		return errors.New("every adapter must provide a structured event stream")
	}
	if len(c.AuthModes) == 0 {
		return errors.New("an adapter must report its auth modes")
	}
	if c.Mode() == agent.ModeUnsupported {
		return fmt.Errorf("capabilities %+v cannot be supervised", c)
	}
	return nil
}

func approveAll() agent.Approver {
	return agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) {
		return agent.Approval{Allow: true}, nil
	})
}

func newSpec(h Harness) agent.StartSpec {
	s := h.Spec()
	s.Approver = approveAll()
	s.ApprovalTimeout = 2 * time.Second
	return s
}

func checkStart(ctx context.Context, h Harness) error {
	bad := newSpec(h)
	bad.Auth = "oauth-from-nowhere"
	if _, err := h.Adapter.Start(ctx, bad); !errors.Is(err, agent.ErrUnsupportedAuth) {
		return fmt.Errorf("an auth mode the adapter does not offer: %s, want ErrUnsupportedAuth", show(err))
	}
	if h.Adapter.Capabilities().HostApprovals {
		noApprover := newSpec(h)
		noApprover.Approver = nil
		if _, err := h.Adapter.Start(ctx, noApprover); !errors.Is(err, agent.ErrNoApprover) {
			return fmt.Errorf("an adapter that routes approvals started without an approver: %s, want ErrNoApprover", show(err))
		}
	}
	return nil
}

func checkEvents(ctx context.Context, h Harness) error {
	h.Scenarios.Finish("all done")
	s, err := h.Adapter.Start(ctx, newSpec(h))
	if err != nil {
		return err
	}
	events, err := collect(s, 5*time.Second)
	if err != nil {
		return err
	}
	if len(events) == 0 || !hasText(events, agent.EventMessage, "all done") {
		return fmt.Errorf("events = %+v, want the agent's message among them", events)
	}
	for _, e := range events {
		if !knownKinds[e.Kind] {
			return fmt.Errorf("event kind %q is not one of the contract's", e.Kind)
		}
		if e.At.IsZero() {
			return fmt.Errorf("event %+v has no time", e)
		}
	}
	res, err := s.Wait()
	if err != nil {
		return fmt.Errorf("Wait: %w", err)
	}
	if res.Status != agent.ResultCompleted || res.SessionID == "" {
		return fmt.Errorf("result = %+v, want completed with a session ID", res)
	}
	return nil
}

// approvalRun runs a session that asks for approval of a tool and returns its
// events.
func approvalRun(ctx context.Context, h Harness, input string, ap agent.Approver, timeout, wait time.Duration) ([]agent.Event, error) {
	if !h.Adapter.Capabilities().HostApprovals {
		return nil, ErrSkip
	}
	h.Scenarios.AskApproval("Bash", input)
	spec := newSpec(h)
	spec.Approver, spec.ApprovalTimeout = ap, timeout
	s, err := h.Adapter.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	return collect(s, wait)
}

func checkApprovalAllowed(ctx context.Context, h Harness) error {
	var mu sync.Mutex
	var got agent.ApprovalRequest
	ap := agent.ApproverFunc(func(_ context.Context, req agent.ApprovalRequest) (agent.Approval, error) {
		mu.Lock()
		got = req
		mu.Unlock()
		return agent.Approval{Allow: true}, nil
	})
	events, err := approvalRun(ctx, h, "ls -la", ap, 2*time.Second, 5*time.Second)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if got.Tool != "Bash" || got.Input != "ls -la" {
		return fmt.Errorf("the approver saw %+v, want the tool and its input", got)
	}
	rec, err := approvalOf(events)
	if err != nil {
		return err
	}
	if !rec.Allow || rec.ID != got.ID {
		return fmt.Errorf("approval record %+v, want an allow for request %q", rec, got.ID)
	}
	return nil
}

func checkApprovalDenied(ctx context.Context, h Harness) error {
	var mu sync.Mutex
	var id string
	ap := agent.ApproverFunc(func(_ context.Context, req agent.ApprovalRequest) (agent.Approval, error) {
		mu.Lock()
		id = req.ID
		mu.Unlock()
		return agent.Approval{Reason: "not in this repository"}, nil
	})
	events, err := approvalRun(ctx, h, "rm -rf build", ap, 2*time.Second, 5*time.Second)
	if err != nil {
		return err
	}
	rec, err := approvalOf(events)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if rec.Allow || rec.ID != id || !strings.Contains(rec.Reason, "not in this repository") {
		return fmt.Errorf("approval record %+v, want a denial of request %q with the human's reason", rec, id)
	}
	return nil
}

func checkApprovalError(ctx context.Context, h Harness) error {
	ap := agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) {
		return agent.Approval{Allow: true}, errors.New("supervisor unreachable")
	})
	events, err := approvalRun(ctx, h, "curl example.invalid", ap, 2*time.Second, 5*time.Second)
	if err != nil {
		return err
	}
	rec, err := approvalOf(events)
	if err != nil {
		return err
	}
	if rec.Allow {
		return fmt.Errorf("an approver error must deny: %+v", rec)
	}
	return nil
}

func checkApprovalTimeout(ctx context.Context, h Harness) error {
	ap := agent.ApproverFunc(func(ctx context.Context, _ agent.ApprovalRequest) (agent.Approval, error) {
		select {
		case <-ctx.Done():
		case <-time.After(30 * time.Second):
		}
		return agent.Approval{Allow: true}, nil // would allow, but far too late
	})
	events, err := approvalRun(ctx, h, "make deploy", ap, 50*time.Millisecond, 3*time.Second)
	if err != nil {
		return err
	}
	rec, err := approvalOf(events)
	if err != nil {
		return fmt.Errorf("no answer within the timeout must end in a recorded denial, and quickly: %w", err)
	}
	if rec.Allow {
		return fmt.Errorf("no answer within the timeout must deny: %+v", rec)
	}
	return nil
}

func checkStopCancelsApproval(ctx context.Context, h Harness) error {
	if !h.Adapter.Capabilities().HostApprovals {
		return ErrSkip
	}
	asked := make(chan struct{})
	cancelled := make(chan struct{})
	ap := agent.ApproverFunc(func(ctx context.Context, _ agent.ApprovalRequest) (agent.Approval, error) {
		close(asked)
		select {
		case <-ctx.Done():
			close(cancelled)
		case <-time.After(20 * time.Second):
		}
		return agent.Approval{Allow: true}, nil // would allow, but the session is gone
	})
	h.Scenarios.AskApproval("Bash", "make deploy")
	spec := newSpec(h)
	spec.Approver, spec.ApprovalTimeout = ap, 20*time.Second
	s, err := h.Adapter.Start(ctx, spec)
	if err != nil {
		return err
	}
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		return errors.New("the approver was never asked")
	}
	if err := s.Stop(ctx); err != nil {
		return fmt.Errorf("Stop: %w", err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		return errors.New("Stop left the approval waiting: the approver's context was not cancelled (D23)")
	}
	events, err := collect(s, 5*time.Second)
	if err != nil {
		return err
	}
	if rec, err := approvalOf(events); err != nil || rec.Allow {
		return fmt.Errorf("a stopped session must record the pending approval as a denial: %+v (%s)", rec, show(err))
	}
	if res, err := s.Wait(); err != nil || res.Status != agent.ResultStopped {
		return fmt.Errorf("result after Stop = %+v (%s), want stopped", res, show(err))
	}
	return nil
}

func checkApprovalCap(ctx context.Context, h Harness) error {
	var mu sync.Mutex
	var seen int
	ap := agent.ApproverFunc(func(_ context.Context, req agent.ApprovalRequest) (agent.Approval, error) {
		mu.Lock()
		seen = len([]rune(req.Input))
		mu.Unlock()
		return agent.Approval{}, nil
	})
	if _, err := approvalRun(ctx, h, strings.Repeat("x", 3*domain.MaxDecisionInput), ap, 2*time.Second, 5*time.Second); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if seen > domain.MaxDecisionInput {
		return fmt.Errorf("the approver saw %d characters of input, the cap is %d", seen, domain.MaxDecisionInput)
	}
	return nil
}

func checkInstruct(ctx context.Context, h Harness) error {
	h.Scenarios.Block()
	s, err := startAndWait(ctx, h, newSpec(h))
	if err != nil {
		return err
	}
	d, err := s.Instruct(ctx, "prefer the standard library")
	if err != nil {
		_ = s.Stop(ctx)
		return fmt.Errorf("Instruct: %w", err)
	}
	_ = s.Stop(ctx)
	if _, err := collect(s, 5*time.Second); err != nil {
		return err
	}
	if h.Adapter.Capabilities().MidRunInstruction {
		if d != agent.DeliveryInjected && d != agent.DeliveryNextTurn {
			return fmt.Errorf("an agent with mid-run injection answered %q, want injected or next_turn", d)
		}
		return nil
	}
	if d != agent.DeliveryResumedTurn {
		return fmt.Errorf("an agent without mid-run injection claimed delivery %q, want resumed_turn", d)
	}
	return nil
}

func checkStopAndResume(ctx context.Context, h Harness) error {
	h.Scenarios.Block()
	s, err := startAndWait(ctx, h, newSpec(h))
	if err != nil {
		return err
	}
	if err := s.Stop(ctx); err != nil {
		return fmt.Errorf("Stop: %w", err)
	}
	if _, err := collect(s, 5*time.Second); err != nil {
		return err
	}
	res, err := s.Wait()
	if err != nil {
		return fmt.Errorf("a stopped session is a result and not an error: %w", err)
	}
	if res.Status != agent.ResultStopped {
		return fmt.Errorf("status after Stop = %q, want stopped", res.Status)
	}
	if res.SessionID == "" {
		return errors.New("a stopped session must report its ID so that it can be resumed")
	}

	if !h.Adapter.Capabilities().SessionResume {
		if _, err := h.Adapter.Resume(ctx, newSpec(h), res.SessionID); !errors.Is(err, agent.ErrUnsupported) {
			return fmt.Errorf("Resume without the capability: %s, want ErrUnsupported", show(err))
		}
		return nil
	}
	h.Scenarios.Finish("picked up")
	r, err := h.Adapter.Resume(ctx, newSpec(h), res.SessionID)
	if err != nil {
		return fmt.Errorf("Resume: %w", err)
	}
	if _, err := collect(r, 5*time.Second); err != nil {
		return err
	}
	if got, err := r.Wait(); err != nil || got.Status != agent.ResultCompleted || got.SessionID != res.SessionID {
		return fmt.Errorf("a resumed session = %+v (%s), want completed in the same session %q", got, show(err), res.SessionID)
	}
	if _, err := h.Adapter.Resume(ctx, newSpec(h), "no-such-session"); !errors.Is(err, agent.ErrNoSession) {
		return fmt.Errorf("Resume of an unknown session: %s, want ErrNoSession", show(err))
	}
	return nil
}

func checkAuthExpired(ctx context.Context, h Harness) error {
	h.Scenarios.AuthExpires()
	s, err := h.Adapter.Start(ctx, newSpec(h))
	if err != nil {
		return err
	}
	events, err := collect(s, 5*time.Second)
	if err != nil {
		return err
	}
	if !hasKind(events, agent.EventAuthExpired) {
		return fmt.Errorf("no auth_expired event: %+v", events)
	}
	res, err := s.Wait()
	if err != nil {
		return fmt.Errorf("an expired login is a result and not an error: %w", err)
	}
	if res.Status != agent.ResultAuthExpired {
		return fmt.Errorf("status = %q, want auth_expired: the run must pause, not fail", res.Status)
	}
	if res.SessionID == "" {
		return errors.New("the session must stay resumable after the login expires")
	}
	return nil
}

func checkQuota(ctx context.Context, h Harness) error {
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	h.Scenarios.QuotaExhausted(reset)
	s, err := h.Adapter.Start(ctx, newSpec(h))
	if err != nil {
		return err
	}
	events, err := collect(s, 5*time.Second)
	if err != nil {
		return err
	}
	if !hasKind(events, agent.EventQuotaExhausted) {
		return fmt.Errorf("no quota_exhausted event: %+v", events)
	}
	res, err := s.Wait()
	if err != nil {
		return fmt.Errorf("exhausted quota is a result and not an error: %w", err)
	}
	if res.Status != agent.ResultQuotaExhausted {
		return fmt.Errorf("status = %q, want quota_exhausted", res.Status)
	}
	if h.Adapter.Capabilities().ReportsQuota && !res.ResetAt.Equal(reset) {
		return fmt.Errorf("the reset time is %v, want %v", res.ResetAt, reset)
	}
	return nil
}

func checkPause(ctx context.Context, h Harness) error {
	h.Scenarios.Block()
	s, err := startAndWait(ctx, h, newSpec(h))
	if err != nil {
		return err
	}
	defer func() {
		_ = s.Stop(ctx)
		_, _ = collect(s, 5*time.Second)
	}()
	p, isPauser := s.(agent.Pauser)
	if claims := h.Adapter.Capabilities().CooperativePause; isPauser != claims {
		return fmt.Errorf("cooperative pause is reported as %v but the session %s a Pauser (D11: the flag must be true)", claims, map[bool]string{true: "is", false: "is not"}[isPauser])
	}
	if isPauser {
		if err := p.Pause(ctx); err != nil {
			return fmt.Errorf("Pause: %w", err)
		}
		if err := p.Continue(ctx); err != nil {
			return fmt.Errorf("Continue: %w", err)
		}
	}
	return nil
}

// NewFakeHarness returns a harness for the fake with the full capabilities.
func NewFakeHarness(*testing.T) Harness { return fakeHarness(FullCaps(), Defects{}) }

// NewDegradedFakeHarness returns a harness for the fake without injection and
// host approvals.
func NewDegradedFakeHarness(*testing.T) Harness { return fakeHarness(DegradedCaps(), Defects{}) }

func fakeHarness(caps agent.Capabilities, defects Defects) Harness {
	f := NewFake(caps)
	f.Defects = defects
	return Harness{
		Adapter:   f,
		Scenarios: f,
		Spec: func() agent.StartSpec {
			return agent.StartSpec{EnvID: "env-1", Workdir: "/work", Prompt: "implement the issue", Auth: agent.AuthSubscription}
		},
	}
}
