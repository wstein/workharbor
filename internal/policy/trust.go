package policy

import "strings"

// Tier says how far the author of an input is trusted (design §6, §7.1, issue
// #53).
type Tier string

const (
	// Trusted is the owner, an organisation member or a collaborator.
	Trusted Tier = "trusted"
	// Untrusted is everyone else, and anything that is not recognised.
	Untrusted Tier = "untrusted"
)

// TierOf reads the author association GitHub reports (the issue's
// author_association) as a tier. It fails closed: only OWNER, MEMBER and
// COLLABORATOR are trusted, in any case, and an empty, new or unknown value is
// untrusted.
func TierOf(association string) Tier {
	switch strings.ToUpper(strings.TrimSpace(association)) {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return Trusted
	}
	return Untrusted
}

// Context is what a policy decision knows besides the action: where the input
// that drives the run came from and what the run can reach.
type Context struct {
	// UntrustedInput is set when text from an untrusted author (an issue, a
	// comment, a CI log) is part of the run.
	UntrustedInput bool
	// PrivateData is set when the run can read private data. The supervisor does
	// not know whether a repository is private, so a caller that cannot tell
	// sets it.
	PrivateData bool
	// Egress is set when the run's environment can make outbound requests
	// (through the proxy's allowlist).
	Egress bool
}

// Trifecta reports whether a run combines private data, untrusted input and
// outbound network: the combination that lets injected instructions send private
// data out (design §7.1). Such a run needs approval.
func (c Context) Trifecta() bool { return c.PrivateData && c.UntrustedInput && c.Egress }

// sensitive are the actions with an effect outside the environment: what leaves
// the host. An untrusted input must not trigger one on its own.
var sensitive = map[Action]bool{PushAgentBranch: true, OpenPR: true, CommentIssue: true}

// DecideIn is Decide with a context: when the input is untrusted, a sensitive
// action that the table lets run on its own (`auto`) asks instead, and when the
// run is a trifecta every action that would run on its own asks. It is never
// looser than Decide: forbid stays forbid, ask stays ask.
func (t Table) DecideIn(a Action, c Context) Mode {
	m := t.Decide(a)
	if m != Auto {
		return m
	}
	if (c.UntrustedInput && sensitive[a]) || c.Trifecta() {
		return Ask
	}
	return m
}
