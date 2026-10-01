package runtimetest

import (
	"context"
	"testing"
)

// The fake is the first backend to pass the conformance suite.
func TestFakePassesTheSuite(t *testing.T) {
	Run(t, NewFakeHarness)
}

// The suite is only worth something if it fails an adapter that lacks a
// guarantee. Each defect switches one guarantee off, and exactly the check
// that guards it must fail.
func TestSuiteNoticesDefectiveAdapters(t *testing.T) {
	tests := []struct {
		name   string
		defect Defects
		check  string
	}{
		{"accepts a spec that was not prepared", Defects{AcceptUnprepared: true}, "provision takes only a prepared spec"},
		{"leaves the network and sidecar after a delete", Defects{LeaveResources: true}, "the network, volume and sidecar are created and removed"},
		{"lets two running environments write one volume", Defects{ShareVolumes: true}, "a writable volume has one running writer"},
		{"mounts something other than what was prepared", Defects{DropMounts: true}, "the prepared mounts are what the runtime mounts"},
		{"lists every owner's environments", Defects{ListAll: true}, "list returns only the owner's environments"},
		{"acts on foreign environments", Defects{TouchForeign: true}, "foreign environments are off limits"},
		{"deletes a running environment", Defects{DeleteRunning: true}, "delete needs a stopped environment"},
		{"ignores stdin", Defects{IgnoreStdin: true}, "exec passes stdin to the command, also while it runs"},
		{"leaves the guest process running after a cancel", Defects{CancelLeavesRun: true}, "cancelling an exec ends the process in the guest"},
		{"keeps environments running through a restart", Defects{RestartKeepsRun: true}, "a service restart leaves every environment stopped"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failed := Failures(context.Background(), func() Harness { return fakeHarness(t, tc.defect) })
			if _, ok := failed[tc.check]; !ok {
				t.Errorf("the suite did not fail %q for an adapter that %s; failed: %v", tc.check, tc.name, failed)
			}
		})
	}
}

func TestCleanFakeHasNoFailures(t *testing.T) {
	if failed := Failures(context.Background(), func() Harness { return fakeHarness(t, Defects{}) }); len(failed) != 0 {
		t.Errorf("the fake failed: %v", failed)
	}
}
