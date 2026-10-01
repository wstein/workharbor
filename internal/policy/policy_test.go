package policy

import "testing"

func TestDefault(t *testing.T) {
	tests := []struct {
		action Action
		want   Mode
	}{
		{Commit, Auto},
		{PushAgentBranch, Ask},
		{OpenPR, Auto},
		{CommentIssue, Auto},
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
