package policy

import "fmt"

// Preset is a repository's workflow (design D47, §6): a named setting of the
// policy table, of where approved commits go, of the PR rule and of the agent's
// permission mode. It is chosen in the supervisor's configuration, never by the
// repository, and it always stays above the fixed floor: a per-SHA approval
// before anything leaves, and no merge, tag, release or deploy by an agent.
type Preset string

// The three presets.
const (
	Prototype   Preset = "prototype"
	Integration Preset = "integration"
	Published   Preset = "published"
)

// DefaultPreset is what a repository gets when the configuration says nothing.
const DefaultPreset = Integration

// ParsePreset reads a preset name; an empty one is the default and anything else
// that is not a preset is an error.
func ParsePreset(s string) (Preset, error) {
	switch Preset(s) {
	case "":
		return DefaultPreset, nil
	case Prototype, Integration, Published:
		return Preset(s), nil
	}
	return "", fmt.Errorf("%q is not a workflow: prototype, integration or published", s)
}

// Table returns the policy table of the preset. The ceilings of Decide apply to
// it like to any override, so no preset can loosen the floor.
func (p Preset) Table() Table {
	t := Default()
	switch p {
	case Prototype:
		t[OpenPR] = Forbid // no PR: the approved commit goes to the integration branch
		t[FastForwardBranch] = Ask
	default:
		t[FastForwardBranch] = Forbid
	}
	return t
}

// Stricter returns the stricter of two presets: published over integration over
// prototype. A task publishes under the stricter of its own and its repository's
// current preset, so a looser preset never applies to a task already started and
// a stricter one applies at once (D47).
func Stricter(a, b Preset) Preset {
	rank := func(p Preset) int {
		switch p {
		case Published:
			return 2
		case Prototype:
			return 0
		}
		return 1 // integration, and the default
	}
	if a == "" {
		a = DefaultPreset
	}
	if b == "" {
		b = DefaultPreset
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// AgentMode is the permission mode the agent runs in unless the human overrides
// it: published asks for every tool (host approvals), the others run a fixed
// allowlist.
func (p Preset) AgentMode() string {
	if p == Published {
		return "manual"
	}
	return "dontAsk"
}

// OpensPR says whether approved commits go to the forge as a pull request.
func (p Preset) OpensPR() bool { return p != Prototype }

// ToDefaultBranch says whether the pull request targets the repository's default
// branch (published) rather than an integration branch.
func (p Preset) ToDefaultBranch() bool { return p == Published }

// EgressAskedAgain says whether an egress request is asked again whenever its
// source changes, and the suggestions from lockfiles are ignored (published).
func (p Preset) EgressAskedAgain() bool { return p == Published }
