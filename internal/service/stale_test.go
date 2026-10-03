package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// countingRuntime counts the stops and starts of environments and can fail
// every stop.
type countingRuntime struct {
	runtime.Adapter
	stops, starts atomic.Int32
	stopErr       error
}

func (c *countingRuntime) Stop(ctx context.Context, id string) error {
	c.stops.Add(1)
	if c.stopErr != nil {
		return c.stopErr
	}
	return c.Adapter.Stop(ctx, id)
}

func (c *countingRuntime) Start(ctx context.Context, id string) error {
	c.starts.Add(1)
	return c.Adapter.Start(ctx, id)
}

// counting puts the counting runtime under the rig's service.
func (r *rig) counting() *countingRuntime {
	c := &countingRuntime{Adapter: r.rt.Adapter}
	r.svc.rt = c
	return c
}

// pauseStored leaves the run paused in the database, as a crash after the save
// of a pause leaves it.
func (r *rig) pauseStored() {
	r.t.Helper()
	a := r.load()
	must(r.t, a.Pause("r1"))
	_, err := r.store.SaveTask(bg, a)
	must(r.t, err)
}

// A live run whose session is gone after a restart of the service has its
// environment stopped once, started, and then relaunched; a later pass does not
// stop an environment this process started, even for another lost run in it.
func TestALostRunsEnvironmentIsStoppedOnceThenStartedThenRelaunched(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	c := r.counting()
	r.agent.Block()

	rep := r.reconcile()
	if len(rep.Resumed) != 1 || len(rep.Interrupted) != 1 || len(rep.Errors) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if c.stops.Load() != 1 || c.starts.Load() != 1 || r.runState() != domain.RunRunning || r.envState() != domain.EnvRunning {
		t.Errorf("stops %d, starts %d, run %s, env %s", c.stops.Load(), c.starts.Load(), r.runState(), r.envState())
	}

	// The run is lost again (its session ends): this process started the
	// environment, so it is not stopped under the agent it launched.
	r.svc.stopSession("r1")
	r.svc.Wait()
	if r.runState() != domain.RunInterrupted {
		t.Fatalf("run = %s after its session ended", r.runState())
	}
	r.agent.Block()
	rep = r.reconcile()
	if len(rep.Resumed) != 1 || c.stops.Load() != 1 {
		t.Errorf("second loss: report %+v, stops %d; want no further stop", rep, c.stops.Load())
	}
}

// With Stop failing nothing is relaunched; each pass counts an attempt until they
// are used up.
func TestAFailedStopRelaunchesNothingAndCountsAnAttempt(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	launched := len(r.agent.Specs)

	rep := r.reconcile()
	run, _ := r.load().Run("r1")
	if len(rep.Resumed) != 0 || len(rep.Errors) == 0 || run.State != domain.RunInterrupted || run.ResumeAttempts != 1 || c.starts.Load() != 0 || len(r.agent.Specs) != launched {
		t.Fatalf("report %+v, run %s (%d attempts), starts %d", rep, run.State, run.ResumeAttempts, c.starts.Load())
	}
	r.reconcile()
	rep = r.reconcile()
	if r.runState() != domain.RunFailed || len(rep.Failed) != 1 {
		t.Errorf("after the attempts: run %s, report %+v", r.runState(), rep)
	}
}

// A paused run a crash left is resumed only after a stop and a start.
func TestAPausedRunLeftByACrashIsResumedOnlyAfterAStopAndStart(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.pauseStored()
	c := r.counting()
	r.agent.Block()

	if _, err := r.svc.Resume(bg, "t1"); err != nil {
		t.Fatal(err)
	}
	if c.stops.Load() != 1 || c.starts.Load() != 1 || r.runState() != domain.RunRunning {
		t.Errorf("stops %d, starts %d, run %s", c.stops.Load(), c.starts.Load(), r.runState())
	}
	// Resumed again by this process: no second stop.
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	r.agent.Block()
	if _, err := r.svc.Resume(bg, "t1"); err != nil || c.stops.Load() != 1 {
		t.Errorf("second resume: %v, stops %d", err, c.stops.Load())
	}
}

func TestResumeWithAFailingStopLaunchesNothingAndKeepsThePausedRun(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.pauseStored()
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	launched := len(r.agent.Specs)

	if _, err := r.svc.Resume(bg, "t1"); err == nil {
		t.Fatal("resume succeeded with a failing stop")
	}
	if r.runState() != domain.RunPaused || len(r.agent.Specs) != launched || c.starts.Load() != 0 {
		t.Errorf("run %s, launches %d, starts %d", r.runState(), len(r.agent.Specs)-launched, c.starts.Load())
	}
}

// An answer that resumes is held to the same rule, and a failed stop leaves the
// question open.
func TestAnAnswerThatResumesStopsAnEnvironmentThisProcessDidNotStart(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	ans := domain.Response{By: "w", Option: domain.AnswerResume, At: r.clock.now}

	if err := r.svc.AnswerDecision(bg, "auth1", ans); err == nil {
		t.Fatal("the answer resumed with a failing stop")
	}
	d, _ := r.load().Decision("auth1")
	if d.Status != domain.DecisionOpen || r.runState() != domain.RunPaused {
		t.Fatalf("decision %s, run %s; want both as they were", d.Status, r.runState())
	}
	c.stopErr = nil
	r.agent.Block()
	must(t, r.svc.AnswerDecision(bg, "auth1", ans))
	if c.stops.Load() != 2 || c.starts.Load() != 1 || r.runState() != domain.RunRunning {
		t.Errorf("stops %d, starts %d, run %s", c.stops.Load(), c.starts.Load(), r.runState())
	}
}

// Cancelling a paused run in an environment this process did not start stops it,
// so a cancel reaches a leftover agent; one this process started is left alone.
func TestCancellingAPausedRunStopsAnEnvironmentThisProcessDidNotStart(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.pauseStored()
	c := r.counting()
	must(t, r.svc.Cancel(bg, "t1"))
	if c.stops.Load() != 1 || r.envState() != domain.EnvStopped {
		t.Errorf("stops %d, env %s", c.stops.Load(), r.envState())
	}

	r2 := newRig(t)
	r2.pauseStored()
	c2 := r2.counting()
	r2.svc.markEnvStarted(r2.env)
	must(t, r2.svc.Cancel(bg, "t1"))
	if c2.stops.Load() != 0 {
		t.Errorf("an environment this process started was stopped %d times", c2.stops.Load())
	}
}
