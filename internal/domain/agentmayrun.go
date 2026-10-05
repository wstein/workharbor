package domain

import (
	"fmt"
	"time"
)

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

// RaiseAgentMayRun records a stop failure and its enduring acknowledgement notice.
// The run must exist, but may already be terminal. Seeing it has no task effect.
func (a *TaskAggregate) RaiseAgentMayRun(id ID, notice AgentMayRun, now time.Time) (Decision, error) {
	if _, err := a.run(notice.RunID); err != nil {
		return Decision{}, err
	}
	if _, err := a.decision(id); err == nil {
		return Decision{}, conflict(RuleDecisionID, "decision %s already exists", id)
	}
	d, err := raise(NewDecision{
		ID: id, TaskID: a.task.ID, RunID: notice.RunID,
		Kind: DecisionQuestion, Cause: CauseAgentMayRun, Subject: "The agent may still run",
		Input: fmt.Sprintf("Path: %s\nError: %s", notice.Path, notice.Error), Options: []string{AnswerSeen}, Now: now,
	})
	if err != nil {
		return Decision{}, err
	}
	a.events = append(a.events, NewAgentMayRunEvent(a.task.ID, notice, now))
	a.addDecision(d)
	return *d, nil
}
