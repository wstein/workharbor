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
	ID       string   `json:"id"`
	TaskID   string   `json:"task_id"`
	RunID    string   `json:"run_id,omitempty"`
	Kind     string   `json:"kind"`
	Blocking bool     `json:"blocking"`
	Cause    string   `json:"cause,omitempty"`
	Subject  string   `json:"subject"`
	Input    string   `json:"input,omitempty"` // untrusted data
	SHA      string   `json:"sha,omitempty"`
	Options  []string `json:"options"`
	Status   string   `json:"status"`
	Deadline string   `json:"deadline,omitempty"`
}

func decisionOf(d domain.Decision) decisionView {
	v := decisionView{
		ID: string(d.ID), TaskID: string(d.TaskID), RunID: string(d.RunID), Kind: string(d.Kind), Blocking: d.Blocking,
		Cause: string(d.Cause), Subject: d.Subject, Input: d.Input, SHA: d.SHA, Options: d.Options, Status: string(d.Status),
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
}

type taskView struct {
	taskSummaryView
	Runs      []runView      `json:"runs"`
	Open      []decisionView `json:"open_decisions"`
	Candidate *candidateView `json:"candidate,omitempty"`
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
		out.Candidate = &candidateView{Branch: c.Branch, SHA: c.SHA, PRURL: c.PRURL, CI: string(c.CI), Pushed: c.Pushed}
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
