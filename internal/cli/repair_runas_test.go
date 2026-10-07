package cli

import (
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
)

// A fix of the user phase is for whr's account, even when a host check names
// it; a `whr setup host` fix is the administrator's.
func TestRunAsIsForUserPhaseFixesOnly(t *testing.T) {
	c := repairContext{RunAs: "workharbor"}
	for fix, want := range map[string]string{
		"whr setup --only config-base":   "whr setup --only config-base (run as workharbor)",
		"whr setup":                      "whr setup (run as workharbor)",
		"whr setup host --only firewall": "whr setup host --only firewall",
		"sudo pmset -a sleep 0":          "sudo pmset -a sleep 0",
	} {
		if got := c.command(fix); got != want {
			t.Errorf("%q: %q, want %q", fix, got, want)
		}
	}
}

// The report of `whr setup host` names whr's account for user-phase fixes only
// when the administrator runs it: delete the assignment or negate notWhr and
// this fails.
func TestReportRunAsIsWhrsAccountForTheAdministratorsHostRun(t *testing.T) {
	for _, tc := range []struct {
		phase  doctor.Phase
		notWhr bool
		want   string
	}{
		{doctor.PhaseHost, true, "workharbor"},
		{doctor.PhaseHost, false, ""},
		{doctor.PhaseUser, true, ""},
		{doctor.PhaseUser, false, ""},
	} {
		if got := reportRunAs(tc.phase, tc.notWhr, "workharbor"); got != tc.want {
			t.Errorf("%v notWhr=%v: %q, want %q", tc.phase, tc.notWhr, got, tc.want)
		}
	}
	got := repairContext{RunAs: reportRunAs(doctor.PhaseHost, true, "workharbor")}.command("whr setup --only config-base")
	if got != "whr setup --only config-base (run as workharbor)" {
		t.Errorf("fix %q", got)
	}
}
