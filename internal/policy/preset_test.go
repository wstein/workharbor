package policy

import "testing"

var presets = []Preset{Prototype, Integration, Published}

// The floor holds in every preset: a table built from one is checked like any
// override, so none loosens what §6 fixes.
func TestTheFloorHoldsInEveryPreset(t *testing.T) {
	for _, p := range presets {
		tab := p.Table()
		if err := tab.Validate(); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		for _, a := range []Action{Merge, Tag, Release, Deploy} {
			if got := tab.Decide(a); got != Forbid {
				t.Errorf("%s: %s is %s, want forbid", p, a, got)
			}
		}
		if got := tab.Decide(PushAgentBranch); got == Auto {
			t.Errorf("%s: pushing runs unasked", p)
		}
		if got := tab.Decide(FastForwardBranch); got == Auto {
			t.Errorf("%s: moving a branch runs unasked", p)
		}
		if got := tab.Decide(Commit); got != Auto {
			t.Errorf("%s: a commit in the topic's checkout is %s", p, got)
		}
	}
	// a table that tries to loosen the floor is lowered, whatever the preset says
	loose := Prototype.Table()
	loose[Merge], loose[PushAgentBranch], loose[FastForwardBranch] = Auto, Auto, Auto
	if loose.Decide(Merge) != Forbid || loose.Decide(PushAgentBranch) != Ask || loose.Decide(FastForwardBranch) != Ask {
		t.Error("an override loosened the floor")
	}
}

func TestEachPresetSetsWhatD47Says(t *testing.T) {
	cases := []struct {
		p      Preset
		pr     bool
		toMain bool
		mode   string
		ff     Mode
		egress bool
		openPR Mode
	}{
		{Prototype, false, false, "dontAsk", Ask, false, Forbid},
		{Integration, true, false, "dontAsk", Forbid, false, Auto},
		{Published, true, true, "manual", Forbid, true, Auto},
	}
	for _, c := range cases {
		tab := c.p.Table()
		if c.p.OpensPR() != c.pr || c.p.ToDefaultBranch() != c.toMain || c.p.AgentMode() != c.mode || c.p.EgressAskedAgain() != c.egress ||
			tab.Decide(FastForwardBranch) != c.ff || tab.Decide(OpenPR) != c.openPR {
			t.Errorf("%s: pr %v toDefault %v mode %s ff %s egress %v openPR %s", c.p, c.p.OpensPR(), c.p.ToDefaultBranch(), c.p.AgentMode(), tab.Decide(FastForwardBranch), c.p.EgressAskedAgain(), tab.Decide(OpenPR))
		}
	}
}

func TestParsePreset(t *testing.T) {
	for in, want := range map[string]Preset{"": Integration, "prototype": Prototype, "integration": Integration, "published": Published} {
		if got, err := ParsePreset(in); err != nil || got != want {
			t.Errorf("%q = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"Prototype", "main", "prototype ", "yolo"} {
		if _, err := ParsePreset(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
