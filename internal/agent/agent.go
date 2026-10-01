// Package agent defines the agent adapter contract for supported coding
// agents (design §5.2). The contract is versioned, its capabilities are
// reported and never assumed, and a conformance suite (agenttest) turns the
// claims into verified facts.
package agent

import (
	"context"
	"errors"
	"time"
)

// ContractVersion is the version of this contract. An adapter reports the
// version it implements in Capabilities.
const ContractVersion = 1

// AuthMode is how an agent authenticates.
type AuthMode string

const (
	// AuthAPIKey keeps the key in the host-side proxy and issues it per run (§7.3).
	AuthAPIKey AuthMode = "api-key"
	// AuthSubscription is a consumer-plan login kept in a per-environment auth
	// directory; the CLI refreshes the token itself.
	AuthSubscription AuthMode = "subscription"
)

// Mode says how much of the contract an adapter meets.
type Mode string

const (
	// ModeFull has headless operation, structured events, mid-run injection
	// and host-routed approvals. Claude Code is the first agent that must be.
	ModeFull Mode = "full"
	// ModeDegraded runs headless with structured events but without injection
	// or host approvals: a message becomes a resumed turn and only a fixed
	// allowlist is permitted. The UI labels it.
	ModeDegraded Mode = "degraded"
	// ModeUnsupported cannot be supervised.
	ModeUnsupported Mode = "unsupported"
)

// Capabilities describe what an agent supports. Every flag is a claim the
// conformance suite checks.
type Capabilities struct {
	ContractVersion int

	Headless          bool // runs unattended
	StructuredEvents  bool // typed events; required of every adapter
	MidRunInstruction bool // a message can be sent into a running session
	HostApprovals     bool // permission prompts are routed to the host
	CooperativePause  bool // pause without ending the process (D11: none of the measured agents)
	SessionResume     bool // a stopped session can be resumed by ID
	IssuePRTooling    bool
	AwaitingGuidance  bool // can raise a blocking question
	ReportsQuota      bool // reports quota_exhausted with the reset time

	AuthModes []AuthMode
}

// Mode computes the mode from the capabilities.
func (c Capabilities) Mode() Mode {
	switch {
	case !c.Headless || !c.StructuredEvents:
		return ModeUnsupported
	case c.MidRunInstruction && c.HostApprovals:
		return ModeFull
	default:
		return ModeDegraded
	}
}

// Supports reports whether the agent offers an auth mode.
func (c Capabilities) Supports(m AuthMode) bool {
	for _, have := range c.AuthModes {
		if have == m {
			return true
		}
	}
	return false
}

// EventKind classifies an event.
type EventKind string

const (
	EventSession        EventKind = "session" // the session ID is known
	EventMessage        EventKind = "message"
	EventToolCall       EventKind = "tool_call"
	EventToolResult     EventKind = "tool_result"
	EventDiff           EventKind = "diff"
	EventTestResult     EventKind = "test_result"
	EventUsage          EventKind = "usage"
	EventApproval       EventKind = "approval" // a permission prompt was put to the host
	EventAuthExpired    EventKind = "auth_expired"
	EventQuotaExhausted EventKind = "quota_exhausted"
	EventError          EventKind = "error"
)

// Event is one typed observation from a running agent. Everything in it comes
// from the agent or the repository and is untrusted data.
type Event struct {
	Kind      EventKind
	At        time.Time
	SessionID string
	Text      string
	Tool      string
	Input     string    // capped
	ResetAt   time.Time // for quota_exhausted, when known
	// Approval is set on an approval event: the record the audit entry is
	// written from (design §5.4).
	Approval *ApprovalRecord
}

// ApprovalRecord is what happened to one permission prompt: the request that
// was put to the host and the answer the agent was given. A prompt that no
// human answered is a denial with the reason why.
type ApprovalRecord struct {
	ID     string // the ApprovalRequest ID
	Allow  bool
	Reason string
}

// ResultStatus is how a session ended.
type ResultStatus string

const (
	ResultCompleted ResultStatus = "completed"
	ResultFailed    ResultStatus = "failed"
	// ResultStopped is a hard interrupt; the session stays resumable.
	ResultStopped ResultStatus = "stopped"
	// ResultAuthExpired and ResultQuotaExhausted end a run without failing it:
	// the supervisor opens a blocking Decision (§4.2) and the session stays
	// resumable.
	ResultAuthExpired    ResultStatus = "auth_expired"
	ResultQuotaExhausted ResultStatus = "quota_exhausted"
)

// Result is the outcome of a session.
type Result struct {
	Status    ResultStatus
	SessionID string
	Text      string
	ResetAt   time.Time // for ResultQuotaExhausted, when known
}

// Delivery says how an instruction reached the agent.
type Delivery string

const (
	// DeliveryInjected reached the agent immediately.
	DeliveryInjected Delivery = "injected"
	// DeliveryNextTurn is picked up at the next model step, after the running
	// tool finishes (Claude Code, spike #1).
	DeliveryNextTurn Delivery = "next_turn"
	// DeliveryResumedTurn is the degraded mode: the message becomes a resumed turn.
	DeliveryResumedTurn Delivery = "resumed_turn"
)

// StartSpec describes a session to start or resume.
type StartSpec struct {
	EnvID   string
	Workdir string
	Prompt  string
	Auth    AuthMode
	// Approver answers the agent's permission prompts. An adapter that reports
	// HostApprovals needs one.
	Approver Approver
	// ApprovalTimeout is how long the Approver has; zero means
	// DefaultApprovalTimeout.
	ApprovalTimeout time.Duration
}

// Errors an adapter returns for the same situations.
var (
	ErrUnsupported     = errors.New("the agent does not support this")
	ErrUnsupportedAuth = errors.New("the agent does not offer this auth mode")
	ErrNoApprover      = errors.New("this agent routes approvals to the host and needs an Approver")
	ErrNoSession       = errors.New("no such session")
	ErrNotRunning      = errors.New("the session is not running")
)

// Adapter starts and resumes sessions of one coding agent.
type Adapter interface {
	Name() string
	Capabilities() Capabilities
	Start(ctx context.Context, spec StartSpec) (Session, error)
	// Resume continues a stopped session by ID (ErrUnsupported without
	// SessionResume, ErrNoSession for an unknown ID).
	Resume(ctx context.Context, spec StartSpec, sessionID string) (Session, error)
}

// Session is one running agent. Events is closed when the session has ended.
type Session interface {
	// ID is the agent's session ID. It may be empty until the first user
	// message has been sent (spike #1), and is set in the Result at the latest.
	ID() string
	Events() <-chan Event
	// Instruct sends a message and says how it was delivered. An agent without
	// MidRunInstruction answers DeliveryResumedTurn and never claims the other two.
	Instruct(ctx context.Context, message string) (Delivery, error)
	// Stop is a hard interrupt that leaves the session resumable.
	Stop(ctx context.Context) error
	// Wait blocks until the session ends and returns its result. An end by
	// auth, quota or Stop is a Result and not an error.
	Wait() (Result, error)
}

// Pauser is implemented by a session only if the agent reports
// CooperativePause (D11). Without it, pausing is Stop followed by Resume.
type Pauser interface {
	Pause(ctx context.Context) error
	Continue(ctx context.Context) error
}
