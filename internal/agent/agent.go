// Package agent defines the agent adapter contract for supported coding
// agents. See docs/content/docs/design.md §5.2.
package agent

import "context"

// Capabilities describe what a runner supports. Resume may only be claimed
// for validated runner x backend pairs.
type Capabilities struct {
	Headless          bool
	MidRunInstruction bool
	CooperativePause  bool
	SessionResume     bool
	StructuredEvents  bool
	IssuePRTooling    bool
	AwaitingGuidance  bool
}

// Event is an observation emitted by a running agent.
type Event struct {
	Kind    string // e.g. "progress", "action", "question"
	Payload []byte
}

// Adapter starts and steers one coding agent.
type Adapter interface {
	Name() string
	Capabilities() Capabilities
	Start(ctx context.Context, envID, prompt string) (sessionID string, err error)
	Events(ctx context.Context, sessionID string) (<-chan Event, error)
	Instruct(ctx context.Context, sessionID, message string) error
	Pause(ctx context.Context, sessionID string) error
	Resume(ctx context.Context, sessionID string) error
	Stop(ctx context.Context, sessionID string) error
}
