// Package policy holds the autonomy table: action -> auto | ask | forbid.
// Enforcement belongs to the forge adapter, never to agent prompts.
// See docs/design.md §6.
package policy

// Action is something an agent may attempt.
type Action string

const (
	PushAgentBranch Action = "push_agent_branch"
	OpenPR          Action = "open_pr"
	CommentIssue    Action = "comment_issue"
	Merge           Action = "merge"
	Tag             Action = "tag"
	Release         Action = "release"
	Deploy          Action = "deploy"
)

// Mode says how an action is handled.
type Mode string

const (
	Auto   Mode = "auto"
	Ask    Mode = "ask"
	Forbid Mode = "forbid"
)

// Table maps actions to modes.
type Table map[Action]Mode

// Default returns the default-deny policy.
func Default() Table {
	return Table{
		PushAgentBranch: Auto,
		OpenPR:          Auto,
		CommentIssue:    Auto,
		Merge:           Forbid,
		Tag:             Forbid,
		Release:         Forbid,
		Deploy:          Forbid,
	}
}

// Decide returns the mode for an action; unknown actions are forbidden.
func (t Table) Decide(a Action) Mode {
	if m, ok := t[a]; ok {
		return m
	}
	return Forbid
}
