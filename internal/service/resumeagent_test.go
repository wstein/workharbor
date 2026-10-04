package service

import (
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

// A resume whose agent record cannot be read starts nothing: the agent would
// otherwise work in the default workdir, outside its own worktree (#250).
func TestResumeFailsClosedWhenTheAgentCannotBeRead(t *testing.T) {
	t.Parallel()
	r := newRig(t, withAgent("ghost")) // the run names an agent the store does not hold
	before := r.agent.Started()
	rep, err := r.svc.Reconcile(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Resumed) != 0 {
		t.Errorf("resumed %v, want nothing", rep.Resumed)
	}
	if got := r.agent.Started(); got != before {
		t.Errorf("agent starts = %d, want %d: an agent was launched", got, before)
	}
	if st := r.runState(); st == domain.RunRunning || st == domain.RunStarting {
		t.Errorf("run is %s, want it not started", st)
	}
}
