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
		{"accepts an unhardened spec", Defects{SkipValidate: true}, "an unhardened spec is rejected and creates nothing"},
		{"accepts forbidden mounts", Defects{SkipMountCheck: true}, "forbidden mounts are rejected and create nothing"},
		{"lists every owner's environments", Defects{ListAll: true}, "list returns only the owner's environments"},
		{"acts on foreign environments", Defects{TouchForeign: true}, "foreign environments are off limits"},
		{"deletes a running environment", Defects{DeleteRunning: true}, "delete needs a stopped environment"},
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
