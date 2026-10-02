package domain

import (
	"fmt"
	"time"
)

// EventRepoSize is the audit entry of how many files a run's checkout tracks,
// counted when the run starts (design D39, §4.4): git in an environment gets slow
// on a very large checkout because the folder is a bind mount, and the human
// should hear it before the agent does. It is in the audit tier and in the task's
// own stream.
const EventRepoSize EventKind = "task.repo_size"

// RepoSizeWarnFiles is the count above which the supervisor warns.
const RepoSizeWarnFiles = 50_000

// RepoSize is the payload of EventRepoSize.
type RepoSize struct {
	RunID        ID     `json:"run_id"`
	TrackedFiles int    `json:"tracked_files"`
	Warn         bool   `json:"warn"`
	Message      string `json:"message,omitempty"`
}

// NewRepoSizeEvent returns the audit entry for a checkout of files tracked
// files. Above RepoSizeWarnFiles it carries a warning that says what to expect.
func NewRepoSizeEvent(task, run ID, files int, at time.Time) (Event, error) {
	if task == "" || run == "" || files < 0 {
		return Event{}, invalid("a repository size needs a task, a run and a count that is not negative")
	}
	rs := RepoSize{RunID: run, TrackedFiles: files}
	if files > RepoSizeWarnFiles {
		rs.Warn = true
		rs.Message = fmt.Sprintf("this checkout tracks %d files (more than %d): git in the environment will be slow, because the folder is a bind mount (D39)", files, RepoSizeWarnFiles)
	}
	return newEvent(task, EventRepoSize, rs, at), nil
}
