package cli

import "testing"

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
