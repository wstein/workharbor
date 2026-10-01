// Package domain defines the core workharbor objects: Task, Workspace, Run,
// Environment, Decision, ReviewCandidate and Event. See docs/design.md §4.
package domain

import "time"

// ID is a stable, opaque identifier.
type ID string

// TaskState is the lifecycle state of a Task.
type TaskState string

const (
	TaskQueued           TaskState = "queued"
	TaskRunning          TaskState = "running"
	TaskAwaitingGuidance TaskState = "awaiting_guidance"
	TaskReadyForReview   TaskState = "ready_for_review"
	TaskCompleted        TaskState = "completed"
	TaskCancelled        TaskState = "cancelled"
	TaskFailed           TaskState = "failed"
)

// RunState is the lifecycle state of a Run. Pause is a run state, not an
// environment state.
type RunState string

const (
	RunStarting    RunState = "starting"
	RunRunning     RunState = "running"
	RunPaused      RunState = "paused"
	RunInterrupted RunState = "interrupted"
	RunStopped     RunState = "stopped"
	RunFailed      RunState = "failed"
)

// EnvState is the lifecycle state of an Environment.
type EnvState string

const (
	EnvProvisioning EnvState = "provisioning"
	EnvRunning      EnvState = "running"
	EnvStopped      EnvState = "stopped"
	EnvDeleted      EnvState = "deleted"
)

// Task is the unit of work: an issue, instructions, decisions and results.
type Task struct {
	ID        ID
	Repo      string
	Issue     string
	State     TaskState
	CreatedAt time.Time
}

// Workspace holds the checkout, branch and caches; it may span several runs.
type Workspace struct {
	ID     ID
	TaskID ID
	Branch string
}

// Run is one execution of an agent in an environment.
type Run struct {
	ID          ID
	TaskID      ID
	WorkspaceID ID
	EnvID       ID
	State       RunState
}

// Environment is the container or VM backing a run or workspace.
type Environment struct {
	ID      ID
	Backend string
	State   EnvState
}

// ReviewCandidate ties a prepared revision to its push, PR and CI results,
// keyed by commit SHA so a pass on an earlier revision never marks a later one
// ready. It is created when cleanup pins the SHA, before the push.
type ReviewCandidate struct {
	TaskID ID
	Branch string
	SHA    string
	PRURL  string
	CI     CIState // the pipeline result for SHA; empty means pending
}

// CIState is the result of a CI pipeline for one commit.
type CIState string

const (
	CIPending CIState = "pending"
	CIPassed  CIState = "passed"
	CIFailed  CIState = "failed"
)

// Event is an append-only record: the audit trail, UI feed and CLI stream.
type Event struct {
	ID      ID
	TaskID  ID
	Kind    string
	Payload []byte
	At      time.Time
}
