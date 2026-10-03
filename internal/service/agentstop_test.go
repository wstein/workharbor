package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// gateSession blocks in Stop until the gate opens, then stops the fake agent and
// reports err: an agent stop that hangs and then fails.
type gateSession struct {
	agent.Session
	entered chan struct{}
	gate    chan struct{}
	err     error
	once    sync.Once
}

func (g *gateSession) Stop(ctx context.Context) error {
	g.once.Do(func() { close(g.entered) })
	<-g.gate
	_ = g.Session.Stop(ctx)
	return g.err
}

// gateAgent wraps an adapter so every session it makes is a gateSession.
type gateAgent struct {
	agent.Adapter
	sess *gateSession
	err  error
	made int
}

func (g *gateAgent) wrap(s agent.Session, err error) (agent.Session, error) {
	if err != nil {
		return nil, err
	}
	g.sess = &gateSession{Session: s, entered: make(chan struct{}), gate: make(chan struct{}), err: g.err}
	if g.made++; g.made > 1 {
		close(g.sess.gate) // only the first session hangs
	}
	return g.sess, nil
}

func (g *gateAgent) Start(ctx context.Context, spec agent.StartSpec) (agent.Session, error) {
	return g.wrap(g.Adapter.Start(ctx, spec))
}

func (g *gateAgent) Resume(ctx context.Context, spec agent.StartSpec, id string) (agent.Session, error) {
	return g.wrap(g.Adapter.Resume(ctx, spec, id))
}

// mayRun returns the task's agent-may-still-run events.
func (r *rig) mayRun() []domain.AgentMayRun {
	r.t.Helper()
	out, err := r.svc.agentNotices(bg, "t1")
	must(r.t, err)
	return out
}

// wantEnvStopped checks the first criterion of a path: the failed agent stop
// stops the environment, records it stopped and forgets the start mark; the run
// is stopped.
func (r *rig) wantEnvStopped(c *countingRuntime) {
	r.t.Helper()
	if c.stops.Load() != 1 || r.envState() != domain.EnvStopped || r.runState() != domain.RunStopped {
		r.t.Errorf("stops %d, env %s, run %s; want 1, stopped, stopped", c.stops.Load(), r.envState(), r.runState())
	}
	if r.svc.envStarted(r.env) {
		r.t.Error("the start mark stays after the fallback")
	}
}

// wantForgotten checks that the failed environment stop leaves the mark forgotten,
// the run in its state, and the next pass stops the environment.
func (r *rig) wantForgotten(c *countingRuntime, run domain.RunState) {
	r.t.Helper()
	if r.runState() != run || r.svc.envStarted(r.env) {
		r.t.Fatalf("run %s, start mark %v", r.runState(), r.svc.envStarted(r.env))
	}
	c.stopErr = nil
	if rep := r.reconcile(); len(rep.Errors) != 0 || r.envState() != domain.EnvStopped {
		r.t.Errorf("errors %v, env %s: the next pass did not stop the environment", rep.Errors, r.envState())
	}
}

func TestACancelWhoseAgentStopFailsStopsTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	if err := r.svc.Cancel(bg, "t1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	r.svc.Wait()
	r.wantEnvStopped(c)
	if got := r.load().Task().State; got != domain.TaskCancelled {
		t.Errorf("task %s", got)
	}
}

func TestACancelWhoseEnvironmentStopFailsTooTellsTheHuman(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	err := r.svc.Cancel(bg, "t1")
	if err == nil || !strings.Contains(err.Error(), "agent may still run") {
		t.Fatalf("cancel: %v", err)
	}
	r.svc.Wait()
	if n := len(r.mayRun()); n != 0 {
		t.Errorf("a human's cancel recorded %d events; the error tells the human", n)
	}
	r.wantForgotten(c, domain.RunStopped)
}

func TestABudgetStopWhoseAgentStopFailsStopsTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	r.svc.cfg.Budgets = Budgets{PerTask: Limit{MaxTokens: 100}}
	r.turn(1, 200, 0)
	r.svc.Wait()
	r.wantEnvStopped(c)
	if got := r.load().Task().State; got != domain.TaskFailed {
		t.Errorf("task %s", got)
	}
}

func TestABudgetStopWhoseEnvironmentStopFailsTooIsAnEventOnTheTask(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	var reported []error
	var mu sync.Mutex
	r.svc.cfg.OnError = func(err error) { mu.Lock(); reported = append(reported, err); mu.Unlock() }
	r.liveStopFails()
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	r.svc.cfg.Budgets = Budgets{PerTask: Limit{MaxTokens: 100}}
	r.turn(1, 200, 0)
	r.svc.Wait()
	ev := r.mayRun()
	if len(ev) != 1 || ev[0].RunID != "r1" || ev[0].EnvID != r.env || ev[0].Path != "budget" || !strings.Contains(ev[0].Error, "stop refused") {
		t.Fatalf("events %+v", ev)
	}
	v, err := r.svc.Show(bg, "t1")
	if err != nil || len(v.AgentMayRun) != 1 {
		t.Errorf("show: %+v, %v", v.AgentMayRun, err)
	}
	r.wantForgotten(c, domain.RunStopped)
}

func TestKillAllWhoseAgentStopFailsStopsTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	rep, err := r.svc.KillAll(bg, "werner")
	must(t, err)
	r.svc.Wait()
	if len(rep.Problems) != 0 || len(rep.AgentMayRun) != 0 || len(rep.Cancelled) != 1 {
		t.Errorf("report %+v", rep)
	}
	r.wantEnvStopped(c)
}

func TestKillAllWhoseEnvironmentStopFailsTooReportsTheTask(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	rep, err := r.svc.KillAll(bg, "werner")
	must(t, err)
	r.svc.Wait()
	// The cancel itself reports the environment not stopped, as the refusing
	// runtime makes it: the task is listed once under AgentMayRun.
	if len(rep.AgentMayRun) != 1 || rep.AgentMayRun[0] != "t1" || len(rep.Problems) == 0 {
		t.Errorf("report %+v", rep)
	}
	if ev := r.mayRun(); len(ev) != 1 || ev[0].Path != "kill-all" {
		t.Errorf("events %+v", ev)
	}
	r.wantForgotten(c, domain.RunStopped)
}

// A suspension's failure is an event on the task, not the serve log alone.
func TestASuspensionWhoseEnvironmentStopFailsTooIsAnEventOnTheTask(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	_ = r.svc.suspend(bg, "t1", "r1", domain.CauseQuotaExhausted, time.Time{})
	r.svc.Wait()
	if ev := r.mayRun(); len(ev) != 1 || ev[0].Path != "suspension" || ev[0].RunID != "r1" {
		t.Errorf("events %+v", ev)
	}
}

// A cancel before the session is up: attach stops it, and a failed stop there
// stops the environment too.
func TestAStopBeforeTheSessionIsUpWhoseStopFailsStopsTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.agent.Block()
	sl := mustBegin(r.t, r.svc) // the launch is under way; no session yet
	r.svc.markEnvStarted(r.env)
	c := r.counting()
	if err := r.svc.Cancel(bg, "t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.checkEnvFree(bg, r.env, ""); err == nil {
		t.Error("the environment is free before the session's stop has ended")
	}
	sess, err := r.agent.Resume(bg, spec(), r.session)
	must(t, err)
	r.svc.attach("t1", "r1", sl, stopFailSession{sess, errors.New("exec client lost")})
	r.svc.Wait()
	r.wantEnvStopped(c)
	if err := r.svc.checkEnvFree(bg, r.env, ""); err != nil {
		t.Errorf("the environment is still busy after the fallback: %v", err)
	}
}

// The busy window (design 4.1): a cancel whose agent stop hangs keeps the
// environment busy, so a second task's start there is refused until the agent stop
// and the fallback have ended; the fallback then never stops an environment under
// the second task's agent.
func TestACancelWithAHangingStopKeepsTheEnvironmentBusy(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	ga := &gateAgent{Adapter: r.svc.ag, err: errors.New("exec client lost")}
	r.svc.ag = ga
	w, first := r.create("busy")
	second, err := r.ws.AddAgent(bg, "busy", "runtime", "", "")
	must(t, err)
	task1, _, err := r.ws.StartTask(bg, StartRequest{AgentID: first.ID, Issue: "#1"})
	must(t, err)
	gate := ga.sess
	var open sync.Once
	openGate := func() { open.Do(func() { close(gate.gate) }) }
	t.Cleanup(openGate) // a failed step must not hang the shutdown

	done := make(chan error, 1)
	go func() { done <- r.svc.Cancel(bg, task1) }()
	select {
	case <-gate.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop never started")
	}
	// The run is saved terminal; its agent stop is still under way.
	pollUntil(t, func() bool {
		a, err := r.store.LoadTask(bg, task1)
		return err == nil && a.Task().State == domain.TaskCancelled
	})
	_, _, err = r.ws.StartTask(bg, StartRequest{AgentID: second.ID, Issue: "#2"})
	var conf *domain.ConflictError
	if !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Fatalf("starting task 2 during the stop: %v; want environment busy", err)
	}
	openGate()
	if err := <-done; err != nil {
		t.Fatalf("cancel: %v", err)
	}
	r.svc.Wait()
	// The fallback has ended: the second task starts and its agent lives.
	_, run2, err := r.ws.StartTask(bg, StartRequest{AgentID: second.ID, Issue: "#2"})
	must(t, err)
	info, err := r.rt.Adapter.Inspect(bg, string(w.EnvID))
	if err != nil || info.State != domain.EnvRunning {
		t.Errorf("the environment is %v, %v after the second start", info.State, err)
	}
	if !r.svc.attached(run2) {
		t.Error("the second task's agent is not attached")
	}
}

func pollUntil(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
