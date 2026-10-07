package service

import (
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/notify"
)

// An interrupted run is reported as awaiting a human on every pass, and the
// pass sends nothing and moves nothing (D57, #356).
func TestReconcileReportsInterruptedRunAwaitingHuman(t *testing.T) {
	r := newRig(t)
	must(t, r.rt.Restart(bg))
	before := r.agent.Started()
	for pass := range 3 {
		rep, err := r.svc.Reconcile(bg)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.AwaitingHuman) != 1 || rep.AwaitingHuman[0] != "r1" {
			t.Fatalf("pass %d: AwaitingHuman = %v, want [r1]", pass, rep.AwaitingHuman)
		}
		if r.agent.Started() != before || r.runState() != domain.RunInterrupted {
			t.Fatalf("pass %d: sent or moved: starts=%d state=%s", pass, r.agent.Started(), r.runState())
		}
	}
}

// Each pass alerts for an interrupted run, with its task and run; the serve
// notifier's throttle limits the repeats, and nothing is started.
func TestReconcileAlertsForInterruptedRun(t *testing.T) {
	r := newRig(t)
	n := &recNotifier{}
	r.svc.cfg.Notifier = n
	must(t, r.rt.Restart(bg))
	before := r.agent.Started()
	if _, err := r.svc.Reconcile(bg); err != nil {
		t.Fatal(err)
	}
	got := n.kinds()
	if len(got) != 1 || got[0] != notify.KindRunInterrupted {
		t.Fatalf("kinds = %v, want [run_interrupted]", got)
	}
	if r.agent.Started() != before {
		t.Fatal("alert pass started an agent")
	}
}
