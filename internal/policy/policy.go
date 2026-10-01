// Package policy holds the autonomy table: action -> auto | ask | forbid.
// Enforcement belongs to the forge adapter, never to agent prompts.
// See docs/content/docs/design/security.md §6.
package policy

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

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

// Valid reports whether m is one of the three modes.
func (m Mode) Valid() bool { return m == Auto || m == Ask || m == Forbid }

// rank orders the modes from the most restrictive; an invalid mode ranks as
// forbid.
func (m Mode) rank() int {
	switch m {
	case Auto:
		return 2
	case Ask:
		return 1
	default:
		return 0
	}
}

// ceilings caps what a table can grant, whatever it says: a per-repository
// override can tighten these actions but never loosen them. Merge, tag,
// release and deploy stay forbidden for agents, and an agent never pushes on
// its own: the supervisor pushes after approval, so push is at most ask
// (design §4.5, §6).
var ceilings = map[Action]Mode{
	PushAgentBranch: Ask,
	Merge:           Forbid,
	Tag:             Forbid,
	Release:         Forbid,
	Deploy:          Forbid,
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

// Decide returns the mode for an action. An action the table does not list,
// and any mode that is not auto, ask or forbid, is forbidden, and a mode
// above the action's ceiling is lowered to it. The result is always valid.
func (t Table) Decide(a Action) Mode {
	m, ok := t[a]
	if !ok || !m.Valid() {
		return Forbid
	}
	if c, capped := ceilings[a]; capped && m.rank() > c.rank() {
		return c
	}
	return m
}

// Validate reports every unknown action and every mode that is not auto, ask
// or forbid, for a caller that loads a table from configuration. Decide
// forbids them either way.
func (t Table) Validate() error {
	var problems []string
	for a, m := range t {
		if !known[a] {
			problems = append(problems, fmt.Sprintf("unknown action %q", a))
		}
		if !m.Valid() {
			problems = append(problems, fmt.Sprintf("action %q has invalid mode %q", a, m))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return errors.New("invalid policy table: " + strings.Join(problems, "; "))
}

// known lists every action a table may mention.
var known = map[Action]bool{
	Commit: true, PushAgentBranch: true, OpenPR: true, CommentIssue: true,
	Merge: true, Tag: true, Release: true, Deploy: true,
}
