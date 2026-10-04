package api

import (
	"encoding/json"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

// The views are the wire format. They are written out, with tags, so the
// contract does not move when a domain struct does. Everything that came from an
// agent, a repository or the forge is data and is named as such where it is a
// free text (input, subject).

type taskSummaryView struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`
	Issue     string    `json:"issue"`
	State     string    `json:"state"`
	AgentID   string    `json:"agent_id,omitempty"`
	Agent     string    `json:"agent,omitempty"` // <workspace>/<role>
	CreatedAt time.Time `json:"created_at"`
}

func summaries(in []store.TaskSummary) []taskSummaryView {
	out := make([]taskSummaryView, 0, len(in))
	for _, t := range in {
		out = append(out, taskSummaryView{ID: string(t.ID), Repo: t.Repo, Issue: t.Issue, State: string(t.State), AgentID: string(t.AgentID), Agent: t.Agent, CreatedAt: t.CreatedAt})
	}
	return out
}

type runView struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id,omitempty"`
	EnvID     string `json:"env_id"`
	State     string `json:"state"`
	SessionID string `json:"session_id,omitempty"`
}

type decisionView struct {
	ID       string `json:"id"`
	TaskID   string `json:"task_id"`
	RunID    string `json:"run_id,omitempty"`
	Kind     string `json:"kind"`
	Blocking bool   `json:"blocking"`
	Cause    string `json:"cause,omitempty"`
	Subject  string `json:"subject"`
	Input    string `json:"input,omitempty"` // untrusted data
	// InputTruncated says Input is not all of the agent's input: an approval
	// allows what the agent asked, of which this is only the start.
	InputTruncated bool     `json:"input_truncated,omitempty"`
	SHA            string   `json:"sha,omitempty"`
	Options        []string `json:"options"`
	Status         string   `json:"status"`
	Deadline       string   `json:"deadline,omitempty"`
}

func decisionOf(d domain.Decision) decisionView {
	v := decisionView{
		ID: string(d.ID), TaskID: string(d.TaskID), RunID: string(d.RunID), Kind: string(d.Kind), Blocking: d.Blocking,
		Cause: string(d.Cause), Subject: d.Subject, Input: d.Input, InputTruncated: d.InputTruncated, SHA: d.SHA, Options: d.Options, Status: string(d.Status),
	}
	if v.Options == nil {
		v.Options = []string{}
	}
	if !d.Deadline.IsZero() {
		v.Deadline = d.Deadline.UTC().Format(time.RFC3339)
	}
	return v
}

type candidateView struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
	PRURL  string `json:"pr_url,omitempty"`
	CI     string `json:"ci,omitempty"`
	Pushed bool   `json:"pushed"`
	// The diff stat of the revision; zero when it was not measured.
	Files   int64 `json:"files"`
	Added   int64 `json:"added"`
	Removed int64 `json:"removed"`
}

// checkView is the receipt of the check on the current revision (D51). Output is
// the agent's data, untrusted: a client shows it escaped, never as markup.
type checkView struct {
	SHA        string `json:"sha"`
	Command    string `json:"command"`
	Source     string `json:"source"`
	ExitStatus int    `json:"exit_status"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
}

// publishAttemptView is one failed publish step of the current revision.
type publishAttemptView struct {
	SHA       string `json:"sha"`
	Attempt   int    `json:"attempt"`
	Transient bool   `json:"transient"`
	Error     string `json:"error"`
	RetryAt   string `json:"retry_at,omitempty"`
}

type taskView struct {
	taskSummaryView
	Runs      []runView      `json:"runs"`
	Open      []decisionView `json:"open_decisions"`
	Candidate *candidateView `json:"candidate,omitempty"`
	// Check is the receipt of the check that ran on the candidate.
	Check *checkView `json:"check,omitempty"`
	// PublishAttempts are the candidate's failed publish steps, oldest first.
	PublishAttempts []publishAttemptView `json:"publish_attempts,omitempty"`
	// UsageLine is service.FormatUsageLine for the task, empty when it has no usage.
	UsageLine string `json:"usage_line,omitempty"`
}

func taskOf(v service.TaskView) taskView {
	out := taskView{
		taskSummaryView: taskSummaryView{
			ID: string(v.Task.ID), Repo: v.Task.Repo, Issue: v.Task.Issue, State: string(v.Task.State),
			AgentID: string(v.Task.AgentID), Agent: v.Agent, CreatedAt: v.Task.CreatedAt,
		},
		Runs: make([]runView, 0, len(v.Runs)), Open: make([]decisionView, 0, len(v.Open)),
	}
	for _, r := range v.Runs {
		out.Runs = append(out.Runs, runView{ID: string(r.ID), AgentID: string(r.AgentID), EnvID: string(r.EnvID), State: string(r.State), SessionID: r.SessionID})
	}
	for _, d := range v.Open {
		out.Open = append(out.Open, decisionOf(d))
	}
	if c := v.Candidate; c != nil {
		out.Candidate = &candidateView{Branch: c.Branch, SHA: c.SHA, PRURL: c.PRURL, CI: string(c.CI), Pushed: c.Pushed, Files: c.Files, Added: c.Added, Removed: c.Removed}
	}
	if r := v.Check; r != nil {
		out.Check = &checkView{SHA: r.SHA, Command: r.Command, Source: r.Source, ExitStatus: r.Code, TimedOut: r.TimedOut, DurationMS: r.Millis, Output: r.Output}
	}
	for _, a := range v.PublishAttempts {
		pv := publishAttemptView{SHA: a.SHA, Attempt: a.Attempt, Transient: a.Transient, Error: a.Error}
		if !a.RetryAt.IsZero() {
			pv.RetryAt = a.RetryAt.UTC().Format(time.RFC3339)
		}
		out.PublishAttempts = append(out.PublishAttempts, pv)
	}
	return out
}

type agentView struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	Branch string `json:"branch"`
}

type workspaceView struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Repo        string      `json:"repo"`
	Integration string      `json:"integration"`
	Path        string      `json:"path"`
	Agents      []agentView `json:"agents"`
}

func workspacesOf(in []service.WorkspaceView) []workspaceView {
	out := make([]workspaceView, 0, len(in))
	for _, w := range in {
		v := workspaceView{ID: string(w.Workspace.ID), Name: w.Workspace.Name, Repo: w.Workspace.Repo, Integration: w.Workspace.Integration, Path: w.Workspace.Path, Agents: make([]agentView, 0, len(w.Agents))}
		for _, a := range w.Agents {
			v.Agents = append(v.Agents, agentView{ID: string(a.ID), Role: a.Role, Branch: a.Branch})
		}
		out = append(out, v)
	}
	return out
}

// eventView is one event of a task's stream.
type eventView struct {
	Seq    int64           `json:"seq,omitempty"` // absent on an ephemeral event
	TaskID string          `json:"task_id"`
	Kind   string          `json:"kind"`
	Tier   string          `json:"tier"`
	At     time.Time       `json:"at"`
	Data   json.RawMessage `json:"data,omitempty"`
}

func eventOf(e domain.Event) eventView {
	v := eventView{Seq: e.Seq, TaskID: string(e.TaskID), Kind: string(e.Kind), Tier: string(e.Tier), At: e.At}
	if json.Valid(e.Payload) {
		v.Data = e.Payload
	}
	return v
}

// editorCopyView is the answer to opening a workspace's editor copy.
type editorCopyView struct {
	Path     string   `json:"path"`
	Warnings []string `json:"warnings"` // untrusted: file names from the repository
}

// RebuildReport is what a rebuild answers: the environments and the images, by
// reference and by digest, before and after.
type RebuildReport struct {
	OldEnv    string `json:"old_env"`
	NewEnv    string `json:"new_env"`
	OldImage  string `json:"old_image"`
	NewImage  string `json:"new_image"`
	OldDigest string `json:"old_digest,omitempty"`
	NewDigest string `json:"new_digest,omitempty"`
}

func rebuildView(r service.RebuildResult) RebuildReport {
	return RebuildReport{OldEnv: r.OldEnv, NewEnv: r.NewEnv, OldImage: r.OldImage, NewImage: r.NewImage, OldDigest: r.OldDigest, NewDigest: r.NewDigest}
}

// ShellView is what the agent shell answers (design §7.3, "The sign-in shell"):
// the environment and the variables of a run, nothing else. The terminal never
// passes through the supervisor: the caller hands this to the runtime's own
// interactive exec. It holds no secret and is not stored.
type ShellView struct {
	EnvID   string   `json:"env_id"`
	Runtime string   `json:"runtime"`
	User    string   `json:"user"`
	Dir     string   `json:"dir"`
	Env     []string `json:"env"`
	Cmd     []string `json:"cmd"`
}

func shellView(t service.ShellTarget) ShellView {
	return ShellView{EnvID: t.EnvID, Runtime: t.Runtime, User: t.User, Dir: t.Dir, Env: t.Env, Cmd: t.Cmd}
}
