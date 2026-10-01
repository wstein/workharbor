package policy

import "testing"

func TestTierOfFailsClosed(t *testing.T) {
	for in, want := range map[string]Tier{
		"OWNER": Trusted, "owner": Trusted, " Member ": Trusted, "COLLABORATOR": Trusted,
		"CONTRIBUTOR": Untrusted, "FIRST_TIME_CONTRIBUTOR": Untrusted, "FIRST_TIMER": Untrusted, "MANNEQUIN": Untrusted,
		"NONE": Untrusted, "": Untrusted, "ADMIN": Untrusted, "OWNER2": Untrusted, "owner\n": Trusted, "SOMETHING_NEW": Untrusted,
	} {
		if got := TierOf(in); got != want {
			t.Errorf("TierOf(%q) = %s, want %s", in, got, want)
		}
	}
}

// Table test for each action and context: never looser than the table.
func TestDecideInIsNeverLooserThanTheTable(t *testing.T) {
	tables := map[string]Table{
		"default":  Default(),
		"all auto": {Commit: Auto, PushAgentBranch: Auto, OpenPR: Auto, CommentIssue: Auto, Merge: Auto, Tag: Auto, Release: Auto, Deploy: Auto},
		"all ask":  {Commit: Ask, PushAgentBranch: Ask, OpenPR: Ask, CommentIssue: Ask, Merge: Ask, Tag: Ask, Release: Ask, Deploy: Ask},
		"empty":    {},
	}
	contexts := map[string]Context{
		"trusted":          {},
		"untrusted":        {UntrustedInput: true},
		"untrusted+egress": {UntrustedInput: true, Egress: true},
		"private+egress":   {PrivateData: true, Egress: true},
		"trifecta":         {UntrustedInput: true, PrivateData: true, Egress: true},
	}
	actions := []Action{Commit, PushAgentBranch, OpenPR, CommentIssue, Merge, Tag, Release, Deploy, "made-up"}
	for tn, table := range tables {
		for cn, ctx := range contexts {
			for _, a := range actions {
				base, got := table.Decide(a), table.DecideIn(a, ctx)
				if got.rank() > base.rank() {
					t.Errorf("%s/%s/%s: %s is looser than the table's %s", tn, cn, a, got, base)
				}
				if !got.Valid() {
					t.Errorf("%s/%s/%s: invalid mode %q", tn, cn, a, got)
				}
			}
		}
	}
}

func TestUntrustedInputMakesSensitiveActionsAsk(t *testing.T) {
	all := Table{Commit: Auto, PushAgentBranch: Auto, OpenPR: Auto, CommentIssue: Auto, Merge: Auto}
	untrusted := Context{UntrustedInput: true}
	for a, want := range map[Action]Mode{
		Commit:          Auto, // local to the environment
		PushAgentBranch: Ask,  // the ceiling is ask anyway
		OpenPR:          Ask,
		CommentIssue:    Ask,
		Merge:           Forbid, // the ceiling
	} {
		if got := all.DecideIn(a, untrusted); got != want {
			t.Errorf("untrusted %s = %s, want %s", a, got, want)
		}
		if got := all.DecideIn(a, Context{}); got != all.Decide(a) {
			t.Errorf("trusted %s = %s, want the table's %s", a, got, all.Decide(a))
		}
	}
}

func TestATrifectaMakesEverythingThatRunsAloneAsk(t *testing.T) {
	c := Context{UntrustedInput: true, PrivateData: true, Egress: true}
	if !c.Trifecta() {
		t.Fatal("a trifecta was not recognised")
	}
	d := Default()
	if got := d.DecideIn(Commit, c); got != Ask {
		t.Errorf("commit in a trifecta = %s, want ask", got)
	}
	if got := d.DecideIn(Deploy, c); got != Forbid {
		t.Errorf("deploy in a trifecta = %s, want forbid", got)
	}
	for _, missing := range []Context{
		{UntrustedInput: true, PrivateData: true}, {UntrustedInput: true, Egress: true}, {PrivateData: true, Egress: true},
	} {
		if missing.Trifecta() {
			t.Errorf("%+v is not a trifecta", missing)
		}
		if got := d.DecideIn(Commit, missing); got != Auto {
			t.Errorf("commit with %+v = %s, want auto", missing, got)
		}
	}
}
