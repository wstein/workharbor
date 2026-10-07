package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/initiation"
)

func TestUnattendedReconcileSendsNothing(t *testing.T) {
	for _, scenario := range []string{"crash", "restart", "lost_session", "failed_launch", "quota_reset"} {
		t.Run(scenario, func(t *testing.T) {
			r := newRig(t)
			switch scenario {
			case "crash":
				must(t, r.rt.Restart(bg))
			case "restart":
				r.svc.Shutdown()
			case "lost_session": // the fixture has a live database run with no attached session.
			case "failed_launch":
				must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error {
					if err := a.Interrupt("r1"); err != nil {
						return err
					}
					if err := a.Resume("r1"); err != nil {
						return err
					}
					_, err := a.RecordLaunchFailure("r1", 3)
					return err
				}))
			case "quota_reset":
				a := r.load()
				_, err := a.SuspendRun("r1", domain.CauseQuotaExhausted, t0.Add(time.Hour), "quota", t0)
				must(t, err)
				_, err = r.store.SaveTask(bg, a)
				must(t, err)
				must(t, r.svc.AnswerDecision(userContext(), "quota", domain.Response{By: "user", Option: domain.AnswerResumeAtReset, At: t0}))
				r.clock.now = t0.Add(2 * time.Hour)
			}
			before := r.agent.Started()
			for range 5 {
				// Even an unrelated request's marker cannot leak into recovery.
				rep, err := r.svc.Reconcile(userContext())
				if err != nil {
					t.Fatal(err)
				}
				if len(rep.Resumed) != 0 || r.agent.Started() != before {
					t.Fatalf("unattended send: %+v starts=%d", rep, r.agent.Started())
				}
			}
			run, _ := r.load().Run("r1")
			if scenario == "quota_reset" {
				if run.State != domain.RunPaused {
					t.Fatalf("reset resumed: %s", run.State)
				}
			} else if run.State != domain.RunInterrupted {
				t.Fatalf("unattended state: %s", run.State)
			}
		})
	}
}

func TestUnmarkedManualResumeChangesNothing(t *testing.T) {
	r := newRig(t)
	must(t, r.svc.Pause(bg, "t1"))
	before := r.agent.Started()
	if _, err := r.svc.Resume(bg, "t1"); !errors.Is(err, initiation.ErrNotInitiated) {
		t.Fatal(err)
	}
	if r.runState() != domain.RunPaused || r.agent.Started() != before {
		t.Fatal("unmarked resume changed state")
	}
	r.agent.Block()
	if _, err := r.svc.Resume(userContext(), "t1"); err != nil {
		t.Fatal(err)
	}
	if r.agent.Started() != before+1 {
		t.Fatal("manual resume did not send exactly once")
	}
}

func TestInitiationDenialIsNotALaunchFailure(t *testing.T) {
	r := newRig(t)
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error {
		if err := a.Interrupt("r1"); err != nil {
			return err
		}
		return a.Resume("r1")
	}))
	before, _ := r.load().Run("r1")
	if err := r.svc.recordLaunchFailure(bg, "t1", "r1", initiation.ErrNotInitiated); !errors.Is(err, initiation.ErrNotInitiated) {
		t.Fatal(err)
	}
	after, _ := r.load().Run("r1")
	if before != after {
		t.Fatalf("gate denial counted as launch failure: before=%+v after=%+v", before, after)
	}
}

func TestSayRequiresFreshMarker(t *testing.T) {
	r := newRig(t)
	must(t, r.svc.Pause(bg, "t1"))
	r.agent.Block()
	ctx := userContext()
	if _, err := r.svc.Resume(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Say(ctx, "t1", "replay"); !errors.Is(err, initiation.ErrNotInitiated) {
		t.Fatalf("replayed marker sent a message: %v", err)
	}
	if _, err := r.svc.Say(bg, "t1", "autonomous"); !errors.Is(err, initiation.ErrNotInitiated) {
		t.Fatal(err)
	}
	if _, err := r.svc.Say(userContext(), "t1", "human"); err != nil {
		t.Fatal(err)
	}
}

func TestAnswerDecisionUpgradesInitiationAfterSuccess(t *testing.T) {
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "login", t0)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	ctx := userContext()
	r.agent.Block()
	if err := r.svc.AnswerDecision(ctx, "login", domain.Response{By: "user", Option: domain.AnswerResume, At: t0}); err != nil {
		t.Fatal(err)
	}
	events, err := r.store.EventsOfKind(bg, "t1", domain.EventRunInitiated)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	if !containsText(string(events[0].Payload), `"kind":"decision_answer"`) || !containsText(string(events[0].Payload), `"decision_id":"login"`) {
		t.Fatalf("answer provenance: %s", events[0].Payload)
	}
}

func containsText(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The marker is carried across the detached start, but background cancellation
// and reconciliation do not mint one.
func TestDetachedStartPreservesMarker(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(userContext())
	cancel()
	done := make(chan bool, 1)
	job := r.svc.startDetached(ctx, "detached-test", func(ctx context.Context) error { done <- initiation.Valid(ctx); return nil })
	if job == nil {
		t.Fatal("no detached start")
	}
	<-job.done
	if !<-done {
		t.Fatal("detached start lost initiation")
	}
}

var _ agent.Adapter = (*initiation.Gate)(nil)
