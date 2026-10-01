// Package agenttest has a scripted fake agent and the conformance suite every
// agent adapter must pass (design §5.2). The fake is what the supervisor and
// reconciler are tested against, and each real adapter (Claude Code, Codex
// CLI) runs the same suite against its own scripted scenarios.
package agenttest

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/agent"
)

// FullCaps are the capabilities of an agent that meets the full contract.
func FullCaps() agent.Capabilities {
	return agent.Capabilities{
		ContractVersion:   agent.ContractVersion,
		Headless:          true,
		StructuredEvents:  true,
		MidRunInstruction: true,
		HostApprovals:     true,
		SessionResume:     true,
		AwaitingGuidance:  true,
		ReportsQuota:      true,
		AuthModes:         []agent.AuthMode{agent.AuthAPIKey, agent.AuthSubscription},
	}
}

// DegradedCaps are the capabilities of an agent without injection and host
// approvals, like Codex `exec` (D12).
func DegradedCaps() agent.Capabilities {
	return agent.Capabilities{
		ContractVersion:  agent.ContractVersion,
		Headless:         true,
		StructuredEvents: true,
		SessionResume:    true,
		AuthModes:        []agent.AuthMode{agent.AuthSubscription},
	}
}

// Defects switches off one guarantee of the fake, so tests can show that the
// conformance suite notices an adapter that lacks it.
type Defects struct {
	FailOpenOnError      bool // an approver error allows
	FailOpenOnTimeout    bool // an approver is waited for without a limit
	InjectionLie         bool // Instruct claims injected delivery the agent cannot do
	AuthExpiredFails     bool // auth_expired ends the run as failed
	PauserWithoutFlag    bool // sessions implement Pauser though the flag is false
	StopLosesSession     bool // Stop returns a result without the session ID
	WrongContractVersion bool // capabilities report another version
	NoSessionEvent       bool // the session never reports its ID
}

type scenarioKind int

const (
	scFinish scenarioKind = iota
	scAsk
	scBlock
	scAuth
	scQuota
)

type scenario struct {
	kind  scenarioKind
	text  string
	tool  string
	input string
	reset time.Time
}

// Fake is a scripted agent adapter. Arrange the next session with Finish,
// AskApproval, Block, AuthExpires or QuotaExhausted; a session started with
// nothing arranged finishes with "done".
type Fake struct {
	// Defects is for tests of the suite only.
	Defects Defects

	caps agent.Capabilities

	mu       sync.Mutex
	queue    []scenario
	sessions int
	known    map[string]bool
}

// NewFake returns a fake with the given capabilities.
func NewFake(caps agent.Capabilities) *Fake {
	return &Fake{caps: caps, known: map[string]bool{}}
}

func (f *Fake) arrange(s scenario) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, s)
}

// Finish arranges a session that says text and completes.
func (f *Fake) Finish(text string) { f.arrange(scenario{kind: scFinish, text: text}) }

// AskApproval arranges a session that puts a permission prompt to the host
// and then completes, reporting "allowed" or "denied: <reason>" as the tool result.
func (f *Fake) AskApproval(tool, input string) {
	f.arrange(scenario{kind: scAsk, tool: tool, input: input})
}

// Block arranges a session that runs until it is stopped.
func (f *Fake) Block() { f.arrange(scenario{kind: scBlock}) }

// AuthExpires arranges a session whose login expires.
func (f *Fake) AuthExpires() { f.arrange(scenario{kind: scAuth}) }

// QuotaExhausted arranges a session that runs out of quota, resetting at resetAt.
func (f *Fake) QuotaExhausted(resetAt time.Time) { f.arrange(scenario{kind: scQuota, reset: resetAt}) }

func (f *Fake) next() scenario {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return scenario{kind: scFinish, text: "done"}
	}
	s := f.queue[0]
	f.queue = f.queue[1:]
	return s
}

// Name implements agent.Adapter.
func (f *Fake) Name() string { return "fake" }

// Capabilities implements agent.Adapter.
func (f *Fake) Capabilities() agent.Capabilities {
	c := f.caps
	if f.Defects.WrongContractVersion {
		c.ContractVersion++
	}
	return c
}

func (f *Fake) check(spec agent.StartSpec) error {
	if !f.caps.Supports(spec.Auth) {
		return agent.ErrUnsupportedAuth
	}
	if f.caps.HostApprovals && spec.Approver == nil {
		return agent.ErrNoApprover
	}
	return nil
}

// Start implements agent.Adapter.
func (f *Fake) Start(ctx context.Context, spec agent.StartSpec) (agent.Session, error) {
	if err := f.check(spec); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.sessions++
	id := "session-" + strconv.Itoa(f.sessions)
	f.mu.Unlock()
	return f.launch(ctx, spec, id, true), nil
}

// Resume implements agent.Adapter.
func (f *Fake) Resume(ctx context.Context, spec agent.StartSpec, sessionID string) (agent.Session, error) {
	if !f.caps.SessionResume {
		return nil, agent.ErrUnsupported
	}
	if err := f.check(spec); err != nil {
		return nil, err
	}
	f.mu.Lock()
	ok := f.known[sessionID]
	f.mu.Unlock()
	if !ok {
		return nil, agent.ErrNoSession
	}
	return f.launch(ctx, spec, sessionID, false), nil
}
