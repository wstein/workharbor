package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// stopFailSession stops the fake agent but reports the stop as failed, as a
// runtime that lost the exec client would.
type stopFailSession struct {
	agent.Session
	err error
}

func (s stopFailSession) Stop(ctx context.Context) error {
	_ = s.Session.Stop(ctx)
	return s.err
}

// liveStopFails attaches a live session whose Stop reports a failure.
func (r *rig) liveStopFails() {
	r.t.Helper()
	r.agent.Block()
	sess, err := r.agent.Resume(bg, spec(), r.session)
	must(r.t, err)
	r.svc.attach("t1", "r1", mustBegin(r.t, r.svc), stopFailSession{sess, errors.New("exec client lost")})
	r.svc.markEnvStarted(r.env)
}

// A paused run stays paused when its environment stops or is gone and the
// reconciler passes, and its open quota question stays open (issue #221).
func TestAPausedRunSurvivesAnEnvironmentStopAndTheReconciler(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseQuotaExhausted, time.Time{}, "q1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	launched := len(r.agent.Specs)

	must(t, r.rt.Adapter.Stop(bg, string(r.env))) // an observed stop
	rep := r.reconcile()
	d, _ := r.load().Decision("q1")
	if r.runState() != domain.RunPaused || d.Status != domain.DecisionOpen || len(rep.Interrupted) != 0 || len(rep.Resumed) != 0 {
		t.Fatalf("run %s, question %s, report %+v", r.runState(), d.Status, rep)
	}
	r.reconcile()
	if r.runState() != domain.RunPaused || len(r.agent.Specs) != launched || r.envState() != domain.EnvStopped {
		t.Errorf("run %s, launches %d, env %s", r.runState(), len(r.agent.Specs)-launched, r.envState())
	}
}

// A hard pause survives a service restart and an environment stop, and the
// human's resume starts the stopped environment first.
func TestAPausedRunResumesOnTheHumansResumeInAStoppedEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.pauseStored()
	c := r.counting()
	launched := len(r.agent.Specs)
	rep := r.reconcile() // the first pass of a new process
	if r.runState() != domain.RunPaused || len(rep.Resumed) != 0 || len(r.agent.Specs) != launched || c.starts.Load() != 0 {
		t.Fatalf("run %s, report %+v, starts %d", r.runState(), rep, c.starts.Load())
	}
	r.agent.Block()
	if _, err := r.svc.Resume(bg, "t1"); err != nil {
		t.Fatal(err)
	}
	if r.runState() != domain.RunRunning || r.envState() != domain.EnvRunning || c.starts.Load() != 1 {
		t.Errorf("run %s, env %s, starts %d", r.runState(), r.envState(), c.starts.Load())
	}
	// This process started the environment; the human stops it, and a resume
	// after another pause starts it again.
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	must(t, c.Adapter.Stop(bg, string(r.env)))
	r.reconcile()
	r.agent.Block()
	if _, err := r.svc.Resume(bg, "t1"); err != nil {
		t.Fatal(err)
	}
	if r.runState() != domain.RunRunning || r.envState() != domain.EnvRunning {
		t.Errorf("second resume: run %s, env %s", r.runState(), r.envState())
	}
}

// A paused run in an environment this process did not start has the environment
// stopped by the first pass, without a relaunch, and only once.
func TestTheFirstPassStopsAPausedRunsLeftoverEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.pauseStored()
	c := r.counting()
	launched := len(r.agent.Specs)

	rep := r.reconcile()
	if len(rep.Errors) != 0 || c.stops.Load() != 1 || r.envState() != domain.EnvStopped || r.runState() != domain.RunPaused || len(r.agent.Specs) != launched {
		t.Fatalf("errors %v, stops %d, env %s, run %s", rep.Errors, c.stops.Load(), r.envState(), r.runState())
	}
	r.reconcile()
	if c.stops.Load() != 1 {
		t.Errorf("a stopped environment was stopped again (%d stops)", c.stops.Load())
	}
}

// An environment this process started is left alone by a pass, a paused run's
// included.
func TestAPassLeavesAnEnvironmentThisProcessStartedAlone(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	c := r.counting()
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	r.reconcile()
	if c.stops.Load() != 0 || r.envState() != domain.EnvRunning || r.runState() != domain.RunPaused {
		t.Errorf("stops %d, env %s, run %s", c.stops.Load(), r.envState(), r.runState())
	}
}

// When the agent cannot be stopped, Pause stops the environment instead.
func TestAPauseWhoseAgentStopFailsStopsTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()

	if err := r.svc.Pause(bg, "t1"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	r.svc.Wait()
	if c.stops.Load() != 1 || r.envState() != domain.EnvStopped || r.runState() != domain.RunPaused {
		t.Errorf("stops %d, env %s, run %s", c.stops.Load(), r.envState(), r.runState())
	}
}

// When the environment stop fails too, the run stays paused, the human is told,
// the process forgets it started the environment, and the next pass stops it.
func TestAPauseWhoseEnvironmentStopFailsToIsStoppedByTheNextPass(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	c.stopErr = errors.New("stop refused")

	err := r.svc.Pause(bg, "t1")
	if err == nil || !strings.Contains(err.Error(), "agent may still run") {
		t.Fatalf("pause: %v", err)
	}
	r.svc.Wait()
	if r.runState() != domain.RunPaused || r.svc.envStarted(r.env) {
		t.Fatalf("run %s, started mark %v", r.runState(), r.svc.envStarted(r.env))
	}
	if rep := r.reconcile(); len(rep.Errors) == 0 {
		t.Error("a failing stop reported nothing")
	}
	c.stopErr = nil
	rep := r.reconcile()
	if len(rep.Errors) != 0 || r.envState() != domain.EnvStopped || r.runState() != domain.RunPaused {
		t.Errorf("errors %v, env %s, run %s", rep.Errors, r.envState(), r.runState())
	}
	// A launch there would first stop and start the environment, not skip it.
	if r.svc.envStarted(r.env) {
		t.Error("the environment is marked started")
	}
}
