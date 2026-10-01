package agenttest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/agent"
)

type session struct {
	f      *Fake
	real   string // the agent's session ID
	id     string // what ID reports: empty until the session event when late
	spec   agent.StartSpec
	events chan agent.Event
	instr  chan string
	stop   chan struct{}
	done   chan struct{}

	stopOnce sync.Once
	mu       sync.Mutex
	result   agent.Result
}

type pausable struct{ *session }

// launch starts a session. A new session reports its ID late, with the session
// event, as Claude Code does (spike #1); a resumed one knows it from the start.
func (f *Fake) launch(ctx context.Context, spec agent.StartSpec, id string, late bool) agent.Session {
	s := &session{
		f: f, real: id, id: id, spec: spec,
		events: make(chan agent.Event, 64),
		instr:  make(chan string, 8),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	if late {
		s.id = ""
	}
	f.mu.Lock()
	f.known[id] = true
	f.mu.Unlock()
	go s.run(ctx, f.next())
	if f.caps.CooperativePause || f.Defects.PauserWithoutFlag {
		return pausable{s}
	}
	return s
}

func (s *session) emit(e agent.Event) {
	e.At = time.Now()
	if e.SessionID == "" {
		e.SessionID = s.real
	}
	s.events <- e
}

func (s *session) finish(r agent.Result) {
	if r.SessionID == "" && (!s.f.Defects.StopLosesSession || r.Status != agent.ResultStopped) {
		r.SessionID = s.real
	}
	s.mu.Lock()
	s.result = r
	s.mu.Unlock()
}

func (s *session) run(ctx context.Context, sc scenario) {
	defer close(s.done)
	defer close(s.events)
	if !s.f.Defects.NoSessionEvent {
		s.bind()
		s.emit(agent.Event{Kind: agent.EventSession})
	}

	switch sc.kind {
	case scFinish:
		s.emit(agent.Event{Kind: agent.EventMessage, Text: sc.text})
		s.emitUsage()
		s.finish(agent.Result{Status: agent.ResultCompleted, Text: sc.text})
	case scAsk:
		s.ask(ctx, sc)
	case scBlock:
		s.emit(agent.Event{Kind: agent.EventMessage, Text: "working"})
		for {
			select {
			case msg := <-s.instr:
				s.emit(agent.Event{Kind: agent.EventMessage, Text: "user: " + msg})
			case <-s.stop:
				s.drainInstructions()
				s.finish(agent.Result{Status: agent.ResultStopped})
				return
			case <-ctx.Done():
				s.finish(agent.Result{Status: agent.ResultStopped})
				return
			}
		}
	case scAuth:
		s.emit(agent.Event{Kind: agent.EventAuthExpired, Text: "authentication_failed"})
		if s.f.Defects.AuthExpiredFails {
			s.emit(agent.Event{Kind: agent.EventError, Text: "login expired"})
			s.finish(agent.Result{Status: agent.ResultFailed})
			return
		}
		s.finish(agent.Result{Status: agent.ResultAuthExpired})
	case scQuota:
		reset := time.Time{}
		if s.f.caps.ReportsQuota {
			reset = sc.reset
		}
		s.emit(agent.Event{Kind: agent.EventQuotaExhausted, ResetAt: reset})
		s.finish(agent.Result{Status: agent.ResultQuotaExhausted, ResetAt: reset})
	}
}

// emitUsage reports one turn's usage when the agent claims to.
func (s *session) emitUsage() {
	if !s.f.caps.ReportsUsage {
		return
	}
	if s.f.Defects.UntypedUsage {
		s.emit(agent.Event{Kind: agent.EventUsage, Text: "12 in, 5 out"})
		return
	}
	s.emit(agent.Event{Kind: agent.EventUsage, Usage: &agent.Usage{
		Model: "fake-model-1", Tokens: &agent.TokenCounts{Input: 120, Output: 45, CacheRead: 900, CacheWrite: 30},
		Cost: &agent.Cost{MicroUSD: 18400, Source: agent.CostReported},
		Windows: []agent.UsageWindow{
			{Name: agent.WindowFiveHour, Utilization: 0.25, ResetsAt: time.Now().Add(3 * time.Hour).Truncate(time.Second)},
			{Name: agent.WindowSevenDay, Utilization: 0.08, ResetsAt: time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)},
		},
	}})
}

// drainInstructions hears the messages sent before the stop, which a select
// may have left in the queue.
func (s *session) drainInstructions() {
	for {
		select {
		case msg := <-s.instr:
			s.emit(agent.Event{Kind: agent.EventMessage, Text: "user: " + msg})
		default:
			return
		}
	}
}

func (s *session) ask(ctx context.Context, sc scenario) {
	s.emit(agent.Event{Kind: agent.EventToolCall, Tool: sc.tool, Input: sc.input})
	req := agent.ApprovalRequest{ID: "approval-1", Tool: sc.tool, Input: sc.input}

	// Stop cancels a prompt that waits for a human (D23).
	actx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !s.f.Defects.StopLeavesApproval {
		go func() {
			select {
			case <-s.stop:
				cancel()
			case <-actx.Done():
			}
		}()
	}

	var ap agent.Approval
	switch {
	case s.spec.Mode() == agent.PermissionDontAsk && !s.f.Defects.AllowlistAsks:
		ap = agent.Approval{Reason: "not on the allowlist: denied"}
		if slices.Contains(s.spec.AllowedTools, sc.tool) {
			ap = agent.Approval{Allow: true}
		}
	case s.f.Defects.FailOpenOnError:
		var err error
		if ap, err = s.spec.Approver.Approve(actx, req); err != nil {
			ap = agent.Approval{Allow: true}
		}
	case s.f.Defects.FailOpenOnTimeout:
		ap, _ = s.spec.Approver.Approve(context.Background(), req) // waits for as long as the approver does
	default:
		ap = agent.Ask(actx, s.spec.Approver, s.spec.ApprovalTimeout, req)
	}
	s.emit(agent.Event{Kind: agent.EventApproval, Tool: sc.tool, Input: sc.input, Approval: &agent.ApprovalRecord{ID: req.ID, Allow: ap.Allow, Reason: ap.Reason}})
	text := "denied: " + ap.Reason
	if ap.Allow {
		text = "allowed"
	}
	s.emit(agent.Event{Kind: agent.EventToolResult, Tool: sc.tool, Text: text})
	select {
	case <-s.stop:
		s.finish(agent.Result{Status: agent.ResultStopped})
	default:
		s.finish(agent.Result{Status: agent.ResultCompleted})
	}
}

// bind makes the session ID known, as the first message does for Claude Code.
func (s *session) bind() {
	s.mu.Lock()
	s.id = s.real
	s.mu.Unlock()
}

func (s *session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

func (s *session) Events() <-chan agent.Event { return s.events }

func (s *session) Instruct(_ context.Context, message string) (agent.Delivery, error) {
	select {
	case <-s.done:
		return "", agent.ErrNotRunning
	default:
	}
	select {
	case s.instr <- message:
	default:
	}
	switch {
	case s.f.caps.MidRunInstruction:
		return agent.DeliveryNextTurn, nil
	case s.f.Defects.InjectionLie:
		return agent.DeliveryInjected, nil
	default:
		return agent.DeliveryResumedTurn, nil
	}
}

func (s *session) Stop(context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	return nil
}

func (s *session) Wait() (agent.Result, error) {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result, nil
}

func (p pausable) Pause(context.Context) error    { return nil }
func (p pausable) Continue(context.Context) error { return nil }
