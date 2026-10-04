package serve

import (
	"testing"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/forge/forgetest"
)

// TestNarrowForgeValuesDoNotReachTheAdapter: the dynamic values the consumers get
// (the default-branch reader and the issue source) are not the forge client, so
// no type assertion on them reaches forge.Adapter, forge.FastForwarder or
// forge.BasedPRs (issue #247).
func TestNarrowForgeValuesDoNotReachTheAdapter(t *testing.T) {
	fake := forgetest.NewFake()
	if _, ok := any(fake).(forge.FastForwarder); !ok {
		t.Skip("the fake has no FastForward: the test would prove nothing")
	}
	fa := NewForgeAccess(fake)
	if fa.DefaultBranch == nil {
		t.Fatal("the fake names a default branch, so DefaultBranch is set")
	}
	values := map[string]any{"ForgeAccess.DefaultBranch": fa.DefaultBranch, "Issues": NewIssueAccess(fake)}
	for name, v := range values {
		if _, ok := v.(forge.Adapter); ok {
			t.Errorf("%s asserts to forge.Adapter", name)
		}
		if _, ok := v.(forge.FastForwarder); ok {
			t.Errorf("%s asserts to forge.FastForwarder", name)
		}
		if _, ok := v.(forge.BasedPRs); ok {
			t.Errorf("%s asserts to forge.BasedPRs", name)
		}
	}
	// The assertions the service makes keep working.
	var issues any = NewIssueAccess(fake)
	if _, ok := issues.(forge.QueueReader); !ok {
		t.Error("Issues no longer asserts to forge.QueueReader")
	}
	if _, ok := issues.(forge.DefaultBrancher); !ok {
		t.Error("Issues no longer asserts to forge.DefaultBrancher")
	}
}
