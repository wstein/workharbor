package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/agenttest"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime/runtimetest"
	"github.com/wstein/workharbor/internal/store"
)

var bg = context.Background()

// fakeClock is time that only moves when a test or a Sleep moves it.
type fakeClock struct {
	now    time.Time
	slept  time.Duration
	sleeps int
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	c.slept += d
	c.sleeps++
	return nil
}

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// rig is a service on the fakes: a runtime with one environment, a fake agent
// with one stopped session, and a task running that session.
type rig struct {
	t       *testing.T
	svc     *Service
	store   *store.Store
	rt      runtimetest.Harness
	agent   *agenttest.Fake
	clock   *fakeClock
	env     domain.ID
	session string
	errs    []error
	ids     int
}

func spec() agent.StartSpec {
	return agent.StartSpec{
		EnvID: "env", Workdir: "/work", Prompt: "continue", Auth: agent.AuthSubscription,
		Approver: agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) { return agent.Approval{}, nil }),
	}
}

// rigOption changes how newRig sets the task up.
type rigOption func(*rigSetup)

type rigSetup struct {
	session    string // the session ID recorded on the run; "" records none
	setSession bool
}

// withSession records a session ID on the run other than the real one ("" for none).
func withSession(id string) rigOption {
	return func(o *rigSetup) { o.session, o.setSession = id, true }
}

func newRig(t *testing.T, opts ...rigOption) *rig {
	t.Helper()
	var setup rigSetup
	for _, o := range opts {
		o(&setup)
	}
	r := &rig{t: t, clock: &fakeClock{now: t0}}
	st, err := store.Open(bg, filepath.Join(t.TempDir(), "workharbor.db"), store.WithClock(func() time.Time { return r.clock.now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r.store = st
	r.rt = runtimetest.NewFakeHarness(t)
	r.agent = agenttest.NewFake(agenttest.FullCaps())

	id, err := r.rt.Adapter.Provision(bg, r.rt.NewSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.rt.Adapter.Start(bg, id); err != nil {
		t.Fatal(err)
	}
	r.env = domain.ID(id)

	// A session that ran once, so that the agent can resume it.
	r.agent.Block()
	sess, err := r.agent.Start(bg, spec())
	if err != nil {
		t.Fatal(err)
	}
	for e := range sess.Events() {
		if e.Kind == agent.EventSession {
			r.session = e.SessionID
			break
		}
	}
	_ = sess.Stop(bg)
	for range sess.Events() {
	}

	a := domain.NewTaskAggregate(domain.Task{ID: "t1", Repo: "wstein/workharbor", Issue: "#23", State: domain.TaskRunning, CreatedAt: t0})
	a.AddEnvironment(domain.Environment{ID: r.env, Backend: "fake", State: domain.EnvRunning})
	must(t, a.StartRun(domain.Run{ID: "r1", WorkspaceID: "w1", EnvID: r.env}))
	must(t, a.MarkRunning("r1"))
	recorded := r.session
	if setup.setSession {
		recorded = setup.session
	}
	if recorded != "" {
		must(t, a.RecordSession("r1", recorded))
	}
	if _, err := st.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}

	r.svc = New(st, r.rt.Adapter, r.agent, r.clock, Config{
		Owner:    r.rt.Owner,
		Spec:     func(domain.Task, domain.Run) agent.StartSpec { return spec() },
		NewID:    func() domain.ID { r.ids++; return domain.ID(fmt.Sprintf("d-%d", r.ids)) },
		ReadyCmd: []string{"echo", "ready"},
		OnError:  func(err error) { r.errs = append(r.errs, err) },
	})
	t.Cleanup(r.svc.Shutdown)
	return r
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (r *rig) load() *domain.TaskAggregate {
	r.t.Helper()
	a, err := r.store.LoadTask(bg, "t1")
	if err != nil {
		r.t.Fatal(err)
	}
	return a
}

func (r *rig) runState() domain.RunState {
	run, _ := r.load().Run("r1")
	return run.State
}

func (r *rig) reconcile() Report {
	r.t.Helper()
	rep, err := r.svc.Reconcile(bg)
	if err != nil {
		r.t.Fatal(err)
	}
	return rep
}

// A restart of the runtime service ends every environment: the run is
// interrupted, the environment is started, exec is awaited and the agent is
// resumed from its session (design §5.3).
func TestRuntimeRestartResumesTheAgentFromItsSession(t *testing.T) {
	r := newRig(t)
	r.agent.Block()
	_ = r.rt.Restart(bg)

	rep := r.reconcile()
	if len(rep.Interrupted) != 1 || len(rep.Resumed) != 1 || len(rep.Failed) != 0 || len(rep.Errors) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	a := r.load()
	run, _ := a.Run("r1")
	env, _ := a.Environment(r.env)
	if run.State != domain.RunRunning || env.State != domain.EnvRunning || run.SessionID != r.session {
		t.Errorf("run %s (session %q), env %s", run.State, run.SessionID, env.State)
	}
	info, _ := r.rt.Adapter.Inspect(bg, string(r.env))
	if info.State != domain.EnvRunning {
		t.Errorf("the runtime says %s: the environment was not started", info.State)
	}
	// The log tells the story: the loss, the environment coming back, the resume.
	log, _ := r.store.EventsSince(bg, "t1", 0, 0)
	var kinds []domain.EventKind
	for _, e := range log {
		kinds = append(kinds, e.Kind)
	}
	if !contains(kinds, domain.EventEnvState) || !contains(kinds, domain.EventRunState) {
		t.Errorf("events = %v", kinds)
	}
	// A second pass has nothing to do.
	if rep := r.reconcile(); len(rep.Interrupted)+len(rep.Resumed)+len(rep.Failed) != 0 {
		t.Errorf("a second pass did something: %+v", rep)
	}
}

func contains[T comparable](list []T, want T) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// While the environment is slow to answer exec, the pass waits on the injected
// clock and then resumes.
func TestTheReconcilerWaitsForExec(t *testing.T) {
	r := newRig(t)
	r.agent.Block()
	_ = r.rt.Restart(bg)
	// An environment that answers exec only after 3 attempts: the ready command
	// fails until the clock has moved 300 ms.
	slow := &slowRuntime{runtimeAdapter: r.rt.Adapter, clock: r.clock, readyAt: t0.Add(300 * time.Millisecond)}
	r.svc.rt = slow

	rep := r.reconcile()
	if len(rep.Resumed) != 1 || r.clock.sleeps < 3 {
		t.Errorf("resumed %d, slept %d times; want a resume after waiting", len(rep.Resumed), r.clock.sleeps)
	}
}

// An environment that never answers leaves the run interrupted for the next
// pass; the error is reported, nothing is failed.
func TestAnEnvironmentThatNeverAnswersIsRetriedLater(t *testing.T) {
	r := newRig(t)
	_ = r.rt.Restart(bg)
	r.svc.cfg.ReadyCmd = []string{"exit", "1"}
	r.svc.cfg.ReadyTimeout = time.Second

	rep := r.reconcile()
	if len(rep.Errors) != 1 || len(rep.Resumed) != 0 || len(rep.Failed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got := r.runState(); got != domain.RunInterrupted {
		t.Errorf("run = %s, want interrupted so that the next pass retries", got)
	}
	if r.clock.slept < time.Second {
		t.Errorf("slept %s, want the timeout to run on the injected clock", r.clock.slept)
	}
	// Later the environment answers.
	r.svc.cfg.ReadyCmd = []string{"echo", "ready"}
	r.agent.Block()
	if rep := r.reconcile(); len(rep.Resumed) != 1 {
		t.Errorf("the retry: %+v", rep)
	}
}

func TestARunWithoutASessionFailsAndAsksWhatToDo(t *testing.T) {
	r := newRig(t, withSession("")) // the agent never reported a session
	_ = r.rt.Restart(bg)

	rep := r.reconcile()
	if len(rep.Failed) != 1 || len(rep.Resumed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	got := r.load()
	run, _ := got.Run("r1")
	d := got.Decisions()
	if run.State != domain.RunFailed || len(d) != 1 || d[0].Cause != domain.CauseRunFailed || got.Task().State != domain.TaskAwaitingGuidance {
		t.Errorf("run %s, decisions %+v, task %s", run.State, d, got.Task().State)
	}
}

func TestASessionTheAgentForgotFailsTheRun(t *testing.T) {
	r := newRig(t, withSession("lost-session")) // the agent no longer knows it
	_ = r.rt.Restart(bg)

	rep := r.reconcile()
	if len(rep.Failed) != 1 || r.runState() != domain.RunFailed {
		t.Errorf("report %+v, run %s", rep, r.runState())
	}
}

func TestALostEnvironmentFailsTheRun(t *testing.T) {
	r := newRig(t)
	// The runtime no longer lists the environment (it was removed).
	must(t, r.rt.Restart(bg))
	must(t, r.rt.Adapter.Delete(bg, string(r.env)))

	rep := r.reconcile()
	got := r.load()
	env, _ := got.Environment(r.env)
	if len(rep.Interrupted) != 1 || len(rep.Failed) != 1 || env.State != domain.EnvDeleted || r.runState() != domain.RunFailed {
		t.Errorf("report %+v, env %s, run %s", rep, env.State, r.runState())
	}
}

// A failed resume leaves the run interrupted to try again, and a started
// agent that dies is not forgotten.
func TestAnAgentThatCannotStartLeavesTheRunInterrupted(t *testing.T) {
	r := newRig(t)
	_ = r.rt.Restart(bg)
	r.svc.ag = failingAgent{err: errors.New("agent binary missing")}

	rep := r.reconcile()
	if len(rep.Errors) != 1 || len(rep.Resumed) != 0 || r.runState() != domain.RunInterrupted {
		t.Errorf("report %+v, run %s", rep, r.runState())
	}
}

func TestAnExpiredApprovalIsExpiredAndTheTaskIsFreed(t *testing.T) {
	r := newRig(t)
	a := r.load()
	_, err := a.RaiseDecision(domain.NewDecision{ID: "ap1", RunID: "r1", Kind: domain.DecisionApproval, Blocking: true, Subject: "Bash", Now: r.clock.now})
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	if rep := r.reconcile(); len(rep.Expired) != 0 {
		t.Fatalf("expired too early: %+v", rep)
	}
	r.clock.now = r.clock.now.Add(domain.DefaultApprovalTimeout)
	rep := r.reconcile()
	got := r.load()
	d, _ := got.Decision("ap1")
	if len(rep.Expired) != 1 || d.Status != domain.DecisionExpired || d.Allows("") || got.Task().State != domain.TaskRunning {
		t.Errorf("report %+v, decision %s, task %s", rep, d.Status, got.Task().State)
	}
}

// "Resume at reset" resumes the run when the reset time has passed.
func TestResumeAtReset(t *testing.T) {
	r := newRig(t)
	reset := t0.Add(2 * time.Hour)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseQuotaExhausted, reset, "q1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	r.agent.Block()

	must(t, r.svc.AnswerDecision(bg, "q1", domain.Response{By: "w", Option: domain.AnswerResumeAtReset, At: r.clock.now}))
	if got := r.runState(); got != domain.RunPaused {
		t.Fatalf("run = %s, want paused until the reset", got)
	}
	if rep := r.reconcile(); len(rep.Resumed) != 0 || r.runState() != domain.RunPaused {
		t.Errorf("before the reset: %+v, run %s", rep, r.runState())
	}
	r.clock.now = reset
	rep := r.reconcile()
	if len(rep.Resumed) != 1 || r.runState() != domain.RunRunning {
		t.Errorf("at the reset: %+v, run %s", rep, r.runState())
	}
}

func TestAnsweringResumeRelaunchesTheAgent(t *testing.T) {
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	r.agent.Block()

	must(t, r.svc.AnswerDecision(bg, "auth1", domain.Response{By: "w", Option: domain.AnswerResume, At: r.clock.now}))
	got := r.load()
	if r.runState() != domain.RunRunning || got.Task().State != domain.TaskRunning {
		t.Errorf("run %s, task %s", r.runState(), got.Task().State)
	}
	if err := r.svc.Cancel(bg, "t1"); err != nil {
		t.Fatal(err)
	}
	r.svc.Wait() // a cancel stops the session
	if r.runState() != domain.RunStopped || r.load().Task().State != domain.TaskCancelled {
		t.Errorf("after cancel: run %s, task %s", r.runState(), r.load().Task().State)
	}
}

func TestACancelAnswerCancelsTheTask(t *testing.T) {
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	must(t, r.svc.AnswerDecision(bg, "auth1", domain.Response{By: "w", Option: domain.AnswerCancel, At: r.clock.now}))
	if got := r.load().Task().State; got != domain.TaskCancelled {
		t.Errorf("task = %s", got)
	}
}

// An agent that reports an expired login while it runs suspends its run and
// raises the blocking question, through the aggregate.
func TestAnAuthEventSuspendsTheRun(t *testing.T) {
	r := newRig(t)
	_ = r.rt.Restart(bg)
	r.agent.AuthExpires()
	r.reconcile()
	r.svc.Wait() // the session ends by itself

	got := r.load()
	run, _ := got.Run("r1")
	ds := got.Decisions()
	if run.State != domain.RunPaused || got.Task().State != domain.TaskAwaitingGuidance || len(ds) != 1 || ds[0].Cause != domain.CauseAuthExpired {
		t.Errorf("run %s, task %s, decisions %+v, errors %v", run.State, got.Task().State, ds, r.errs)
	}
}

func TestACompletedSessionStopsTheRun(t *testing.T) {
	r := newRig(t)
	_ = r.rt.Restart(bg)
	r.agent.Finish("all done")
	r.reconcile()
	r.svc.Wait()
	if got := r.runState(); got != domain.RunStopped {
		t.Errorf("run = %s, want stopped", got)
	}
}

// slowRuntime makes the ready command fail until a time has passed.
type slowRuntime struct {
	runtimeAdapter
	clock   *fakeClock
	readyAt time.Time
}

// failingAgent cannot resume anything.
type failingAgent struct{ err error }

func (failingAgent) Name() string                     { return "failing" }
func (failingAgent) Capabilities() agent.Capabilities { return agenttest.FullCaps() }
func (f failingAgent) Start(context.Context, agent.StartSpec) (agent.Session, error) {
	return nil, f.err
}

func (f failingAgent) Resume(context.Context, agent.StartSpec, string) (agent.Session, error) {
	return nil, f.err
}
