package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/notify"
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
	in, ierr := r.store.InboxDecisions(bg)
	if ierr != nil || len(in) != 1 || in[0].Cause != domain.CauseAgentMayRun || in[0].Blocking {
		t.Fatalf("budget notice: %+v, %v", in, ierr)
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
	n := &recNotifier{}
	r.svc.cfg.Notifier = n
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
	in, ierr := r.svc.store.InboxDecisions(bg)
	if ierr != nil || len(in) != 1 || in[0].Cause != "agent_may_run" || in[0].Blocking {
		t.Fatalf("surviving kill-all notice: %+v, %v", in, ierr)
	}
	foundPush := false
	for _, kind := range n.kinds() {
		foundPush = foundPush || kind == notify.KindAgentMayRun
	}
	if !foundPush {
		t.Fatal("agent warning was not sent through the notifier")
	}
	if err := r.svc.AnswerDecision(bg, in[0].ID, domain.Response{Option: domain.AnswerResume, By: "werner", At: t0}); !errors.Is(err, domain.ErrDecisionOption) {
		t.Fatalf("notice accepted resume: %v", err)
	}
	must(t, r.svc.AnswerDecision(bg, in[0].ID, domain.Response{Option: domain.AnswerSeen, By: "werner", At: t0}))
	seen, err := r.store.LoadDecision(bg, in[0].ID)
	must(t, err)
	if seen.AnsweredBy != "werner" || seen.Status != domain.DecisionAnswered || r.load().Task().State != domain.TaskCancelled {
		t.Fatalf("acknowledgement: %+v", seen)
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
	in, err := r.store.InboxDecisions(bg)
	if err != nil || len(in) != 2 {
		t.Fatalf("suspension inbox: %+v, %v", in, err)
	}
	found := false
	for _, d := range in {
		found = found || (d.Cause == domain.CauseAgentMayRun && !d.Blocking)
	}
	if !found {
		t.Fatal("suspension warning missing")
	}
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
	r.agent.Block()
	task2, run2, err := r.ws.StartTask(bg, StartRequest{AgentID: second.ID, Issue: "#2"})
	must(t, err)
	pollUntil(t, func() bool {
		agg, err := r.store.LoadTask(bg, task2)
		must(t, err)
		run, ok := agg.Run(run2)
		if ok && run.State.Terminal() {
			t.Fatalf("the second task's run ended: %s", run.State)
		}
		events, err := r.svc.Log(bg, task2, 0, 0)
		must(t, err)
		for _, event := range events {
			var observed agent.Event
			if json.Unmarshal(event.Payload, &observed) == nil && observed.Kind == agent.EventMessage && observed.Text == "working" {
				return true
			}
		}
		return false
	})
	info, err := r.rt.Adapter.Inspect(bg, string(w.EnvID))
	if err != nil || info.State != domain.EnvRunning {
		t.Errorf("the environment is %v, %v after the second start", info.State, err)
	}
	if !r.svc.attached(run2) {
		t.Error("the second task's agent is not attached")
	}
	if err := r.svc.checkEnvFree(bg, w.EnvID, ""); !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Errorf("the second task's live agent does not own the environment: %v", err)
	}
	if stops := r.agent.Stops(); stops != 1 {
		t.Errorf("agent stops %d; want only the first agent stopped", stops)
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

// An answer "cancel" to a Decision with no cause leaves the task and the run as
// they are (the domain cancels only for a cause), so it must not take the run's
// environment: the agent stop's fallback would stop it under a live run.
func TestACancelAnswerWithoutACauseDoesNotStopTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.liveStopFails()
	c := r.counting()
	a := r.load()
	_, err := a.RaiseDecision(domain.NewDecision{
		ID: "q1", RunID: "r1", Kind: domain.DecisionQuestion, Blocking: true,
		Subject: "go on?", Options: []string{domain.AnswerCancel, "go"}, Now: r.clock.now,
	})
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	must(t, r.svc.AnswerDecision(bg, "q1", domain.Response{Option: domain.AnswerCancel, By: "werner", At: r.clock.now}))
	r.svc.Wait()
	if c.stops.Load() != 0 || r.envState() == domain.EnvStopped {
		t.Errorf("stops %d, env %s: the environment of a live run was stopped", c.stops.Load(), r.envState())
	}
	if err := r.svc.checkNotHeld(r.env); err != nil {
		t.Errorf("a hold was left on the environment: %v", err)
	}
}

// The human's cancel of a run that is still starting returns before attach has
// stopped the agent, so a failure there is an event on the task, not the serve
// log alone.
func TestAStopBeforeTheSessionIsUpWhoseEnvironmentStopFailsTooIsAnEvent(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.agent.Block()
	sl := mustBegin(r.t, r.svc)
	r.svc.markEnvStarted(r.env)
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	if err := r.svc.Cancel(bg, "t1"); err != nil {
		t.Fatal(err)
	}
	sess, err := r.agent.Resume(bg, spec(), r.session)
	must(t, err)
	r.svc.attach("t1", "r1", sl, stopFailSession{sess, errors.New("exec client lost")})
	r.svc.Wait()
	if ev := r.mayRun(); len(ev) != 1 || ev[0].Path != "cancel" || ev[0].RunID != "r1" {
		t.Errorf("events %+v", ev)
	}
	in, err := r.store.InboxDecisions(bg)
	if err != nil || len(in) != 1 || in[0].Cause != domain.CauseAgentMayRun {
		t.Fatalf("late cancel notice: %+v, %v", in, err)
	}
}

// A failed record after a good environment stop is no surviving agent: it is an
// error, but not an agent-may-run one.
func TestAFailedRecordAfterAGoodEnvironmentStopIsNotAnAgentMayRun(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	err := r.svc.stopEnvAfterAgent(bg, "t1", "no-such-env", "cancelled", errors.New("exec client lost"))
	if err == nil || agentMayRun(err) {
		t.Errorf("record failure: %v, mayRun %v; want a plain error", err, agentMayRun(err))
	}
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	err = r.svc.stopEnvAfterAgent(bg, "t1", r.env, "cancelled", errors.New("exec client lost"))
	if !agentMayRun(err) || !strings.Contains(err.Error(), "agent may still run") {
		t.Errorf("failed runtime stop: %v; want an agent-may-run error", err)
	}
}

func TestKillAllReportsAStartingRunWhoseAgentStopIsPending(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error {
		if err := a.Interrupt("r1"); err != nil {
			return err
		}
		return a.Resume("r1")
	}))
	r.agent.Block()
	sl := mustBegin(t, r.svc)
	r.svc.markEnvStarted(r.env)
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	rep, err := r.svc.KillAll(bg, "werner")
	must(t, err)
	raw, err := json.Marshal(rep)
	must(t, err)
	if !strings.Contains(string(raw), `"agent_stop_pending":["t1"]`) {
		t.Errorf("pending stop missing: %s", raw)
	}
	sess, err := r.agent.Resume(bg, spec(), r.session)
	must(t, err)
	r.svc.attach("t1", "r1", sl, stopFailSession{sess, errors.New("exec client lost")})
	r.svc.Wait()
	in, err := r.svc.store.InboxDecisions(bg)
	if err != nil || len(in) != 1 || in[0].Cause != "agent_may_run" {
		t.Fatalf("deferred notice: %+v, %v", in, err)
	}
}
