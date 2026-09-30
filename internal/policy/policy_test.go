package policy

import "testing"

func TestDefaultDeny(t *testing.T) {
	p := Default()
	for _, a := range []Action{Merge, Tag, Release, Deploy, "unknown"} {
		if p.Decide(a) != Forbid {
			t.Errorf("%s must be forbidden by default", a)
		}
	}
	if p.Decide(OpenPR) != Auto {
		t.Error("open_pr should be auto")
	}
}
