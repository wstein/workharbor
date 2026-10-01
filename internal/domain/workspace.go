package domain

import (
	"regexp"
	"strings"
	"time"
)

// Rules of the workspace and agent records (design §4.3, D42).
const (
	RuleWorkspaceName Rule = "workspace-name" // a workspace is named by lowercase letters, digits and "-"
	RuleAgentRole     Rule = "agent-role"     // an agent's role follows the same rule and is unique in its workspace
	RuleEnvBusy       Rule = "env-busy"       // one active run per environment until several agents run at once (#94)
)

// Workspace is a folder on the host or an external SSD with the agent clone the
// agents work in, mounted into one long-lived environment (design D42). It is
// created and removed by the human and outlives every task.
type Workspace struct {
	ID          ID
	Name        string // unique
	Path        string // the host folder
	Repo        string // "owner/name": where the agent clone was seeded from
	Integration string // "main" or "develop": what agents rebase onto
	EnvID       ID     // empty until the environment exists
	CreatedAt   time.Time
}

// Agent is a named role in a workspace: its worktree and branch, instructions,
// permission profile and session. The agent persists; the tasks assigned to it
// do not.
type Agent struct {
	ID          ID
	WorkspaceID ID
	Role        string // unique in the workspace
	Branch      string // agent/<role>, derived
	Worktree    string // /ws/wt/<role> in the environment, derived
	// Instructions is the agent's standing instruction text, from the
	// supervisor's configuration and never from the repository (design D38).
	Instructions string
	// Profile names a permission profile of the policy; empty means the default.
	Profile string
	// SessionID is the agent's current session, recorded when it reports one.
	SessionID string
}

// WorktreeRoot is where the agents' worktrees live in the environment.
const WorktreeRoot = "/ws/wt"

var (
	workspaceNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	agentRoleRE     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)
	repoRE          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
)

// WorkspaceStream is the key of a workspace's events in the event log, next to
// the task IDs it also holds.
func WorkspaceStream(id ID) ID { return "workspace:" + id }

// WorkspaceAdded is the payload of EventWorkspaceAdded and EventWorkspaceRemoved.
type WorkspaceAdded struct {
	ID          ID     `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Integration string `json:"integration,omitempty"`
}

// AgentAdded is the payload of EventAgentAdded and EventAgentRemoved.
type AgentAdded struct {
	ID          ID     `json:"id"`
	WorkspaceID ID     `json:"workspace_id"`
	Role        string `json:"role"`
	Branch      string `json:"branch,omitempty"`
}

// NewWorkspace validates a workspace and returns it with the audit event of its
// creation. The path must be absolute and clean; whether it lies under a
// workspace root is the configuration's check (design §4.4).
func NewWorkspace(id ID, name, path, repo, integration string, now time.Time) (Workspace, Event, error) {
	switch {
	case id == "":
		return Workspace{}, Event{}, invalid("a workspace needs an ID")
	case !workspaceNameRE.MatchString(name):
		return Workspace{}, Event{}, conflict(RuleWorkspaceName, "workspace name %q: use lowercase letters, digits and -, starting with a letter, at most 40 characters", name)
	case !strings.HasPrefix(path, "/") || path != cleanPath(path):
		return Workspace{}, Event{}, invalid("workspace path " + quote(path) + " must be absolute and clean")
	case !repoRE.MatchString(repo):
		return Workspace{}, Event{}, invalid("repository " + quote(repo) + " must look like owner/name")
	case integration != "main" && integration != "develop":
		return Workspace{}, Event{}, invalid("integration branch " + quote(integration) + " must be main or develop")
	}
	w := Workspace{ID: id, Name: name, Path: path, Repo: repo, Integration: integration, CreatedAt: now.UTC()}
	ev := newEvent(WorkspaceStream(id), EventWorkspaceAdded, WorkspaceAdded{ID: id, Name: name, Path: path, Repo: repo, Integration: integration}, now)
	return w, ev, nil
}

// NewAgent validates a role and returns the agent with the branch and worktree
// derived from it, and the audit event of its creation. The role is the only
// input to either, and it is restricted, so a name taken from an issue or from
// the agent's own output never becomes a path or a ref.
func NewAgent(id, workspaceID ID, role, instructions, profile string, now time.Time) (Agent, Event, error) {
	switch {
	case id == "" || workspaceID == "":
		return Agent{}, Event{}, invalid("an agent needs an ID and a workspace")
	case !agentRoleRE.MatchString(role):
		return Agent{}, Event{}, conflict(RuleAgentRole, "agent role %q: use lowercase letters, digits and -, starting with a letter, at most 30 characters", role)
	}
	a := Agent{
		ID: id, WorkspaceID: workspaceID, Role: role,
		Branch: "agent/" + role, Worktree: WorktreeRoot + "/" + role,
		Instructions: instructions, Profile: profile,
	}
	ev := newEvent(WorkspaceStream(workspaceID), EventAgentAdded, AgentAdded{ID: id, WorkspaceID: workspaceID, Role: role, Branch: a.Branch}, now)
	return a, ev, nil
}

// RemovedWorkspaceEvent and RemovedAgentEvent are the audit events of a removal.
func RemovedWorkspaceEvent(w Workspace, now time.Time) Event {
	return newEvent(WorkspaceStream(w.ID), EventWorkspaceRemoved, WorkspaceAdded{ID: w.ID, Name: w.Name}, now)
}

// RemovedAgentEvent is the audit event of an agent's removal.
func RemovedAgentEvent(a Agent, now time.Time) Event {
	return newEvent(WorkspaceStream(a.WorkspaceID), EventAgentRemoved, AgentAdded{ID: a.ID, WorkspaceID: a.WorkspaceID, Role: a.Role, Branch: a.Branch}, now)
}

// Live reports whether a run holds its environment: starting, running or
// paused. A paused run keeps the environment, because the human may be working
// in it (design §4.3).
func (r Run) Live() bool {
	return r.State == RunStarting || r.State == RunRunning || r.State == RunPaused
}

// CheckEnvironmentFree is the rule of one active run per environment (design
// §4.3): runs are every run in that environment, of any task. It returns the
// conflict env-busy naming the run in the way, or nil.
func CheckEnvironmentFree(env ID, runs []Run) error {
	for _, r := range runs {
		if r.EnvID == env && r.Live() {
			return conflict(RuleEnvBusy, "environment %s is busy: run %s of agent %q is %s; several agents at once are not supported yet", env, r.ID, r.AgentID, r.State)
		}
	}
	return nil
}

func cleanPath(p string) string {
	// path.Clean without importing path for one use
	parts := strings.Split(p, "/")
	var out []string
	for _, s := range parts {
		switch s {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, s)
		}
	}
	return "/" + strings.Join(out, "/")
}

func quote(s string) string { return `"` + s + `"` }
