package policy

import (
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	tests := []struct {
		action Action
		want   Mode
	}{
		{Commit, Auto},
		{PushAgentBranch, Ask},
		{OpenPR, Auto},
		{CommentIssue, Auto},
		{UpdateBoard, Auto},
		{Merge, Forbid},
		{Tag, Forbid},
		{Release, Forbid},
		{Deploy, Forbid},
		{"unknown", Forbid},
	}
	p := Default()
	for _, tt := range tests {
		if got := p.Decide(tt.action); got != tt.want {
			t.Errorf("Decide(%s) = %s, want %s", tt.action, got, tt.want)
		}
	}
}

func TestHardFloorCannotBeLoosened(t *testing.T) {
	for _, a := range []Action{Merge, Tag, Release, Deploy} {
		for _, m := range []Mode{Auto, Ask} {
			p := Default()
			p[a] = m
			if got := p.Decide(a); got != Forbid {
				t.Errorf("override %s=%s: Decide = %s, want forbid", a, m, got)
			}
		}
	}
}

func TestOverrideCanTighten(t *testing.T) {
	p := Default()
	p[OpenPR] = Forbid
	if got := p.Decide(OpenPR); got != Forbid {
		t.Errorf("Decide(open_pr) = %s, want forbid", got)
	}
}

// #51: an override that set push_agent_branch to auto took effect.
func TestPushIsAtMostAsk(t *testing.T) {
	tests := []struct {
		set  Mode
		want Mode
	}{
		{Auto, Ask},
		{Ask, Ask},
		{Forbid, Forbid}, // an override may tighten it
	}
	for _, tt := range tests {
		p := Default()
		p[PushAgentBranch] = tt.set
		if got := p.Decide(PushAgentBranch); got != tt.want {
			t.Errorf("override push=%s: Decide = %s, want %s", tt.set, got, tt.want)
		}
	}
}

// #51: Decide returned any stored mode unchanged, so a caller that tested
// `!= Forbid` let "" or "AUTO" through.
func TestUnknownModesAreForbidden(t *testing.T) {
	for _, bad := range []Mode{"", "AUTO", "Ask", " ask", "ask ", "yes", "allow", "deny", "0"} {
		for _, a := range []Action{Commit, PushAgentBranch, OpenPR, CommentIssue, Merge} {
			p := Default()
			p[a] = bad
			if got := p.Decide(a); got != Forbid {
				t.Errorf("override %s=%q: Decide = %q, want forbid", a, bad, got)
			}
		}
	}
}

func TestDecideAlwaysReturnsAValidMode(t *testing.T) {
	p := Table{Commit: "", OpenPR: "maybe", PushAgentBranch: "auto", CommentIssue: Auto}
	for _, a := range []Action{Commit, OpenPR, PushAgentBranch, CommentIssue, Merge, "unknown"} {
		if m := p.Decide(a); !m.Valid() {
			t.Errorf("Decide(%s) = %q, which is not a valid mode", a, m)
		}
	}
}

func TestValidateReportsWhatDecideWouldForbid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Errorf("the default table is invalid: %v", err)
	}
	p := Default()
	p[OpenPR] = "maybe"
	p["delete_repo"] = Auto
	err := p.Validate()
	if err == nil {
		t.Fatal("a table with an unknown mode and an unknown action must not validate")
	}
	for _, want := range []string{"open_pr", "maybe", "delete_repo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestACeilingStillAllowsTighteningEverywhere(t *testing.T) {
	for _, a := range []Action{Commit, PushAgentBranch, OpenPR, CommentIssue, Merge, Tag, Release, Deploy} {
		p := Default()
		p[a] = Forbid
		if got := p.Decide(a); got != Forbid {
			t.Errorf("override %s=forbid: Decide = %s", a, got)
		}
	}
}
