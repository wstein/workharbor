// Package policy holds the autonomy table: action -> auto | ask | forbid.
// Enforcement belongs to the forge adapter, never to agent prompts.
// See docs/content/docs/design.md §6.
package policy

// Action is something an agent may attempt.
type Action string

const (
	Commit          Action = "commit"
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

// hardFloor lists actions that stay forbidden for agents whatever a
// table says; a per-repository override can never loosen them.
var hardFloor = map[Action]bool{
	Merge:   true,
	Tag:     true,
	Release: true,
	Deploy:  true,
}

// Table maps actions to modes.
type Table map[Action]Mode

// Default returns the default-deny policy. The agent commits in its own
// checkout; the supervisor pushes the prepared branch only after a human
// approves it (design §4.5), so pushing asks.
func Default() Table {
	return Table{
		Commit:          Auto,
		PushAgentBranch: Ask,
		OpenPR:          Auto,
		CommentIssue:    Auto,
		Merge:           Forbid,
		Tag:             Forbid,
		Release:         Forbid,
		Deploy:          Forbid,
	}
}

// Decide returns the mode for an action. Unknown actions and the hard
// floor are forbidden.
func (t Table) Decide(a Action) Mode {
	if hardFloor[a] {
		return Forbid
	}
	if m, ok := t[a]; ok {
		return m
	}
	return Forbid
}
