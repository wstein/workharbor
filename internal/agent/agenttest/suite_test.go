package agenttest

import (
	"context"
	"testing"
)

// The fake passes the suite in both modes: full, and degraded, where the
// checks that need injection or host approvals skip.
func TestFakePassesTheSuite(t *testing.T) {
	t.Run("full", func(t *testing.T) { Run(t, NewFakeHarness) })
	t.Run("degraded", func(t *testing.T) { Run(t, NewDegradedFakeHarness) })
}

func TestDegradedFakeSkipsOnlyWhatItDoesNotClaim(t *testing.T) {
	ctx := context.Background()
	skipped := 0
	for _, c := range Checks() {
		if err := c.Fn(ctx, NewDegradedFakeHarness(t)); err != nil {
			if err != ErrSkip { //nolint:errorlint // ErrSkip is returned unwrapped
				t.Errorf("%s: %v", c.Name, err)
				continue
			}
			skipped++
		}
	}
	// The six approval checks need HostApprovals, which a degraded agent does not claim.
	if skipped != 6 {
		t.Errorf("%d checks skipped, want the 6 that need host approvals", skipped)
	}
}

// The suite is only worth something if it fails an adapter that lacks a
// guarantee. Each defect switches one guarantee off, and exactly the check
// that guards it must fail.
func TestSuiteNoticesDefectiveAgents(t *testing.T) {
	tests := []struct {
		name   string
		defect Defects
		check  string
	}{
		{"allows when the approver fails", Defects{FailOpenOnError: true}, "an approver error denies"},
		{"waits for an approver without a limit", Defects{FailOpenOnTimeout: true}, "an approver that does not answer in time denies"},
		{"claims injected delivery it cannot do", Defects{InjectionLie: true}, "instruction delivery is honest"},
		{"fails the run when the login expires", Defects{AuthExpiredFails: true}, "auth expiry ends the run without failing it"},
		{"offers a Pauser without the flag", Defects{PauserWithoutFlag: true}, "cooperative pause is a capability flag"},
		{"loses the session ID on stop", Defects{StopLosesSession: true}, "stop is a hard interrupt and the session is resumable"},
		{"never reports its session ID", Defects{NoSessionEvent: true}, "the session ID arrives with the session event"},
		{"leaves a pending approval waiting on stop", Defects{StopLeavesApproval: true}, "stop cancels a pending approval"},
		{"implements another contract version", Defects{WrongContractVersion: true}, "capabilities are reported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caps := FullCaps()
			if tc.defect.InjectionLie {
				caps = DegradedCaps() // the lie is about a capability it lacks
			}
			failed := Failures(context.Background(), func() Harness { return fakeHarness(caps, tc.defect) })
			if _, ok := failed[tc.check]; !ok {
				t.Errorf("the suite did not fail %q for an adapter that %s; failed: %v", tc.check, tc.name, failed)
			}
		})
	}
}

func TestCleanFakeHasNoFailures(t *testing.T) {
	if failed := Failures(context.Background(), func() Harness { return fakeHarness(FullCaps(), Defects{}) }); len(failed) != 0 {
		t.Errorf("the fake failed: %v", failed)
	}
}
