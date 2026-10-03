package domain

import "time"

// EventAgentMayRun is the audit entry of an agent stop that failed together with
// the stop of its environment: the agent may still run (design 4.1, "No surviving
// agent before a relaunch", fifth path). It is recorded on the task when no human
// call waits for the answer, so `whr show` and the inbox can say so with the run,
// not only the supervisor's log.
const EventAgentMayRun EventKind = "agent.may_run"

// AgentMayRun is the payload of EventAgentMayRun. It holds the errors' text, never
// a secret: an adapter's error names no token.
type AgentMayRun struct {
	RunID ID     `json:"run_id"`
	EnvID ID     `json:"env_id"`
	Path  string `json:"path"` // suspension, budget or kill-all
	Error string `json:"error"`
}

// NewAgentMayRunEvent returns the audit entry of an agent that may still run.
func NewAgentMayRunEvent(task ID, a AgentMayRun, at time.Time) Event {
	return newEvent(task, EventAgentMayRun, a, at)
}
