package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/agenttest"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/notify"
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
	errMu   sync.Mutex
	errs    []error // what OnError got, possibly from service goroutines; read through reported
	ids     atomic.Int64
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
	task       func(*domain.Task) // changes the task before it is saved
	agent      domain.ID          // the agent the task and its run belong to
}

// withAgent assigns the task and its run to an agent, whose record the test adds.
func withAgent(id domain.ID) rigOption {
	return func(o *rigSetup) { o.agent = id }
}

// withTask changes the task the rig saves (its workflow, its branch).
func withTask(f func(*domain.Task)) rigOption {
	return func(o *rigSetup) { o.task = f }
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

	prep, err := r.rt.Prepare(r.rt.NewSpec())
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.rt.Adapter.Provision(bg, prep)
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

	task := domain.Task{ID: "t1", Repo: "wstein/workharbor", Issue: "#23", State: domain.TaskRunning, CreatedAt: t0}
	task.AgentID = setup.agent
	if setup.task != nil {
		setup.task(&task)
	}
	a := domain.NewTaskAggregate(task)
	a.AddEnvironment(domain.Environment{ID: r.env, Backend: "fake", State: domain.EnvRunning})
	must(t, a.StartRun(domain.Run{ID: "r1", WorkspaceID: "w1", AgentID: setup.agent, EnvID: r.env}))
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
		NewID:    func() domain.ID { return domain.ID(fmt.Sprintf("d-%d", r.ids.Add(1))) },
		ReadyCmd: []string{"echo", "ready"},
		OnError:  func(err error) { r.errMu.Lock(); r.errs = append(r.errs, err); r.errMu.Unlock() },
	})
	t.Cleanup(func() {
		// Bounded: a session that is no longer in s.sessions (a bug under test
		// orphaned it) is never stopped, and Shutdown would wait for it forever.
		done := make(chan struct{})
		go func() { r.svc.Shutdown(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("Shutdown did not return within 5s: an agent session is still running and no longer held by the service")
		}
	})
	return r
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// live attaches an agent session to the run, as a running supervisor has, so
// the reconciler does not take the run for a lost one.
func (r *rig) live() {
	r.t.Helper()
	r.agent.Block()
	sess, err := r.agent.Resume(bg, spec(), r.session)
	must(r.t, err)
	r.svc.attach("t1", "r1", mustBegin(r.t, r.svc), sess)
	r.svc.markEnvStarted(r.env) // an attached agent was launched by this process
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	r := newRig(t, withSession("lost-session")) // the agent no longer knows it
	_ = r.rt.Restart(bg)

	rep := r.reconcile()
	if len(rep.Failed) != 1 || r.runState() != domain.RunFailed {
		t.Errorf("report %+v, run %s", rep, r.runState())
	}
}

func TestALostEnvironmentFailsTheRun(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	r := newRig(t)
	_ = r.rt.Restart(bg)
	r.svc.ag = failingAgent{err: errors.New("agent binary missing")}

	rep := r.reconcile()
	if len(rep.Errors) != 1 || len(rep.Resumed) != 0 || r.runState() != domain.RunInterrupted {
		t.Errorf("report %+v, run %s", rep, r.runState())
	}
}

func TestAnExpiredApprovalIsExpiredAndTheTaskIsFreed(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	r := newRig(t)
	_ = r.rt.Restart(bg)
	r.agent.AuthExpires()
	r.reconcile()
	r.svc.Wait() // the session ends by itself

	got := r.load()
	run, _ := got.Run("r1")
	ds := got.Decisions()
	if run.State != domain.RunPaused || got.Task().State != domain.TaskAwaitingGuidance || len(ds) != 1 || ds[0].Cause != domain.CauseAuthExpired {
		t.Errorf("run %s, task %s, decisions %+v, errors %v", run.State, got.Task().State, ds, r.reported())
	}
}

// Suspending is a pause (design 4.2): the run is saved paused with its question,
// then the agent is stopped, and the session's end is not taken for a loss.
func TestAnAuthEventStopsTheAgentOfTheSuspendedRun(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	_ = r.rt.Restart(bg)
	base := r.agent.Stops() // the rig's own setup stops one
	r.agent.AuthExpires()
	r.reconcile()
	r.svc.Wait()
	if n := r.agent.Stops(); n != base+1 {
		t.Errorf("stops = %d, want %d", n, base+1)
	}
	if got := r.runState(); got != domain.RunPaused {
		t.Errorf("run = %s", got)
	}
	if ds := r.load().Decisions(); len(ds) != 1 || ds[0].Status != domain.DecisionOpen {
		t.Errorf("decisions %+v", ds)
	}
	// a repeated event changes nothing and stops nothing
	must(t, r.svc.suspend(bg, "t1", "r1", domain.CauseAuthExpired, time.Time{}))
	if n := r.agent.Stops(); n != base+1 || len(r.load().Decisions()) != 1 {
		t.Errorf("a repeat: stops %d, decisions %d", n, len(r.load().Decisions()))
	}
}

func TestACompletedSessionStopsTheRun(t *testing.T) {
	t.Parallel()
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

// After a supervisor restart the container is still up, but the agent process
// the supervisor owned is gone: the run is lost although the environment
// looks fine (D6).
func TestARunWithoutASessionIsLostEvenWithAnEnvironmentThatIsUp(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.agent.Block()
	rep := r.reconcile() // a fresh service: nothing is attached
	if len(rep.Interrupted) != 1 || len(rep.Resumed) != 1 || r.runState() != domain.RunRunning {
		t.Errorf("report %+v, run %s", rep, r.runState())
	}
	if len(r.agent.Specs) < 2 {
		t.Error("the agent was not resumed")
	}
	// A run whose agent is attached is left alone.
	if rep := r.reconcile(); len(rep.Interrupted) != 0 || len(rep.Resumed) != 0 {
		t.Errorf("an attached run was disturbed: %+v", rep)
	}
}

// A shutdown ends the sessions, and the runs resume on the next start.
func TestShutdownInterruptsRunsInsteadOfStoppingThem(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	r.svc.Shutdown()
	got := r.load()
	run, _ := got.Run("r1")
	if run.State != domain.RunInterrupted || got.Task().State != domain.TaskRunning {
		t.Fatalf("after a shutdown: run %s, task %s; want interrupted and running", run.State, got.Task().State)
	}
	r.agent.Block()
	if rep := r.reconcile(); len(rep.Resumed) != 1 || r.runState() != domain.RunRunning {
		t.Errorf("the next start: %+v, run %s", rep, r.runState())
	}
}

func TestResumingAfterANoSessionAnswerFailsTheRun(t *testing.T) {
	t.Parallel()
	r := newRig(t, withSession("gone"))
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	if err := r.svc.AnswerDecision(bg, "auth1", domain.Response{By: "w", Option: domain.AnswerResume, At: r.clock.now}); !errors.Is(err, agent.ErrNoSession) {
		t.Fatalf("answer = %v, want ErrNoSession", err)
	}
	if got := r.runState(); got != domain.RunFailed {
		t.Errorf("run = %s, want failed rather than stuck in starting", got)
	}
}

func TestAChosenWaitForTheResetSurvivesAnInterruption(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	reset := t0.Add(2 * time.Hour)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseQuotaExhausted, reset, "q1", r.clock.now)
	must(t, err)
	must(t, a.Answer("q1", domain.Response{By: "w", Option: domain.AnswerResumeAtReset, At: r.clock.now}))
	must(t, a.Interrupt("r1"))
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	must(t, r.rt.Restart(bg)) // the environment is stopped too

	rep := r.reconcile()
	if len(rep.Resumed) != 0 || r.runState() != domain.RunInterrupted || r.envState() != domain.EnvStopped {
		t.Fatalf("before the reset: %+v, run %s, env %s; it must wait and start nothing", rep, r.runState(), r.envState())
	}
	r.clock.now = reset
	r.agent.Block()
	if rep := r.reconcile(); len(rep.Resumed) != 1 || r.runState() != domain.RunRunning {
		t.Errorf("at the reset: %+v, run %s", rep, r.runState())
	}
}

func (r *rig) envState() domain.EnvState {
	info, err := r.rt.Adapter.Inspect(bg, string(r.env))
	must(r.t, err)
	return info.State
}

// A run that waits for a human costs no container.
func TestNoContainerIsStartedForARunWaitingOnTheHuman(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	a := r.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r.clock.now)
	must(t, err)
	must(t, a.Interrupt("r1"))
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	must(t, r.rt.Restart(bg))
	for range 3 {
		rep := r.reconcile()
		if len(rep.Errors) != 0 || len(rep.Resumed) != 0 || r.envState() != domain.EnvStopped {
			t.Fatalf("report %+v, env %s: the login question is open, so nothing may start", rep, r.envState())
		}
	}
}

func TestEveryResumeStartsWithTheBriefing(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.live()
	a := r.load()
	_, err := a.RaiseDecision(domain.NewDecision{ID: "ap1", RunID: "r1", Kind: domain.DecisionApproval, Blocking: true, Subject: "Bash", Input: "make deploy", Now: r.clock.now})
	must(t, err)
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	r.svc.Shutdown() // interrupts the run and supersedes the approval
	r.agent.Block()
	r.reconcile()

	specs := r.agent.Specs
	last := specs[len(specs)-1]
	if !strings.HasPrefix(last.Prompt, "Supervisor briefing") {
		t.Fatalf("the first message does not start with the briefing:\n%s", last.Prompt)
	}
	for _, want := range []string{"unknown", "partial", "Bash", "make deploy", "superseded", "Check the workspace first", "continue"} {
		if !strings.Contains(last.Prompt, want) {
			t.Errorf("the briefing lacks %q:\n%s", want, last.Prompt)
		}
	}
}

// A failing agent is retried a few times and then the run fails.
func TestAttemptsAreCountedAndUsedUp(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	must(t, r.rt.Restart(bg))
	r.svc.ag = failingAgent{err: errors.New("agent binary missing")}
	for attempt := 1; attempt <= 2; attempt++ {
		rep := r.reconcile()
		if len(rep.Errors) != 1 || r.runState() != domain.RunInterrupted {
			t.Fatalf("attempt %d: %+v, run %s", attempt, rep, r.runState())
		}
	}
	rep := r.reconcile()
	got := r.load()
	run, _ := got.Run("r1")
	if len(rep.Failed) != 1 || run.State != domain.RunFailed || len(got.Decisions()) != 1 {
		t.Errorf("third attempt: %+v, run %s, decisions %d", rep, run.State, len(got.Decisions()))
	}
}

// The old session's deferred cleanup must not remove a newer session's entry.
func TestAnOldSessionDoesNotRemoveANewerEntry(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	old := mustBegin(t, r.svc)
	r.svc.end("r1", old)
	fresh := mustBegin(t, r.svc) // a relaunch during a pass
	r.svc.end("r1", old)
	if !r.svc.attached("r1") {
		t.Fatal("the old session's cleanup removed the new entry")
	}
	r.svc.end("r1", fresh)
	if r.svc.attached("r1") {
		t.Error("the entry was not removed by its own session")
	}
}

type pushes struct{ got []notify.Message }

func (p *pushes) Notify(_ context.Context, m notify.Message) error {
	p.got = append(p.got, m)
	return nil
}

// A blocking Decision, a login that expired and a failed run each notify once,
// with IDs and a kind only (design §9.4).
func TestBlockingDecisionsAndEndsNotify(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	rec := &pushes{}
	r.svc.cfg.Notifier = notify.Throttled{Next: rec, Throttle: &notify.Throttle{Now: func() time.Time { return r.clock.now }}}
	r.live()

	must(t, r.svc.update(bg, "t1", func(x *domain.TaskAggregate) error {
		_, e := x.RaiseDecision(domain.NewDecision{ID: "ap1", RunID: "r1", Kind: domain.DecisionApproval, Blocking: true, Subject: "Bash", Input: "SECRET-INPUT", Now: r.clock.now})
		if e != nil {
			return e
		}
		_, e = x.RaiseDecision(domain.NewDecision{ID: "q-quiet", RunID: "r1", Kind: domain.DecisionQuestion, Now: r.clock.now})
		return e
	}))
	if len(rec.got) != 1 || rec.got[0].Kind != notify.KindApproval || rec.got[0].DecisionID != "ap1" || rec.got[0].TaskID != "t1" {
		t.Fatalf("pushes = %+v, want one approval and none for the quiet question", rec.got)
	}

	// A cancel does not notify the human who asked for it.
	rec.got = nil
	must(t, r.svc.Cancel(bg, "t1"))
	for _, m := range rec.got {
		if m.Kind == notify.KindRunEnded {
			t.Errorf("a cancel pushed %+v", m)
		}
	}
}

func TestAFailedRunNotifies(t *testing.T) {
	t.Parallel()
	r := newRig(t, withSession(""))
	rec := &pushes{}
	r.svc.cfg.Notifier = rec
	must(t, r.rt.Restart(bg))
	r.reconcile() // no session: the run fails and asks what to do
	found := false
	for _, m := range rec.got {
		found = found || m.Kind == notify.KindRunFailed
	}
	if !found {
		t.Errorf("pushes = %+v, want run_failed", rec.got)
	}
}

// #79: a session attached after Shutdown began is stopped at once and never
// joins the wait group, so Shutdown neither misses it nor races wg.Wait.
func TestSessionAttachedDuringShutdownIsStopped(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.agent.Block()
	sess, err := r.agent.Start(bg, spec())
	must(t, err)
	r.svc.Shutdown() // no sessions yet: returns at once, and the service is closing
	sl := mustBegin(t, r.svc)
	r.svc.attach("t1", "r1", sl, sess)

	done := make(chan agent.Result, 1)
	go func() { res, _ := sess.Wait(); done <- res }()
	select {
	case res := <-done:
		if res.Status != agent.ResultStopped {
			t.Fatalf("the late session ended %s, want stopped", res.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a session attached during shutdown kept running")
	}
	waited := make(chan struct{})
	go func() { r.svc.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait blocked on a session attached during shutdown")
	}
	if r.svc.attached("r1") {
		t.Error("the late session is still registered")
	}
}

// The readiness command runs under what is left of ReadyTimeout, not under the
// caller's context: one that never ends returns at the readiness deadline.
func TestReadyTimeoutBoundsACommandThatNeverEnds(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.svc.clock = SystemClock{}
	r.svc.cfg.ReadyTimeout = 50 * time.Millisecond
	r.svc.cfg.ReadyInterval = 5 * time.Millisecond
	r.svc.cfg.ReadyCmd = []string{"sleep"}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()

	start := time.Now()
	err := r.svc.waitReady(ctx, r.env)
	took := time.Since(start)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("err = %v, ctx = %v; want the readiness error before the caller's deadline", err, ctx.Err())
	}
	if took > 2*time.Second {
		t.Errorf("took %s, want about the 50ms readiness deadline", took)
	}
}

// Repeated failing commands still end at the deadline.
func TestReadyTimeoutEndsRepeatedFailures(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.svc.cfg.ReadyCmd = []string{"exit", "1"}
	r.svc.cfg.ReadyTimeout = time.Second
	if err := r.svc.waitReady(bg, r.env); err == nil {
		t.Fatal("want an error")
	}
	if r.clock.slept < time.Second {
		t.Errorf("slept %s, want the timeout to run on the injected clock", r.clock.slept)
	}
}

// reported returns what the service reported through OnError so far. The service
// reports from goroutines of its own (the board worker, an asking agent), so it
// is read under the lock that OnError writes under.
func (r *rig) reported() []error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	return append([]error(nil), r.errs...)
}
