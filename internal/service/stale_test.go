package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/store"
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

	rep := r.reconcileAndResume()
	if len(rep.Resumed) != 1 || len(rep.Interrupted) != 1 || len(rep.Errors) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if c.stops.Load() != 1 || c.starts.Load() != 1 || r.runState() != domain.RunRunning || !r.svc.attached("r1") || r.envState() != domain.EnvRunning {
		t.Errorf("stops %d, starts %d, run %s, attached %v, env %s", c.stops.Load(), c.starts.Load(), r.runState(), r.svc.attached("r1"), r.envState())
	}

	// The run is lost again (its session ends): this process started the
	// environment, so it is not stopped under the agent it launched.
	r.svc.stopSession("r1")
	r.svc.Wait()
	if r.runState() != domain.RunInterrupted {
		t.Fatalf("run = %s after its session ended", r.runState())
	}
	r.agent.Block()
	rep = r.reconcileAndResume()
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

	rep := r.reconcileAndResume()
	run, _ := r.load().Run("r1")
	if len(rep.Resumed) != 0 || len(rep.Errors) == 0 || run.State != domain.RunInterrupted || run.ResumeAttempts != 1 || c.starts.Load() != 0 || len(r.agent.Specs) != launched {
		t.Fatalf("report %+v, run %s (%d attempts), starts %d", rep, run.State, run.ResumeAttempts, c.starts.Load())
	}
	r.reconcileAndResume()
	rep = r.reconcileAndResume()
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

	if _, err := r.svc.Resume(userContext(), "t1"); err != nil {
		t.Fatal(err)
	}
	if c.stops.Load() != 1 || c.starts.Load() != 1 || r.runState() != domain.RunRunning {
		t.Errorf("stops %d, starts %d, run %s", c.stops.Load(), c.starts.Load(), r.runState())
	}
	// Resumed again by this process: no second stop.
	must(t, r.svc.Pause(bg, "t1"))
	r.svc.Wait()
	r.agent.Block()
	if _, err := r.svc.Resume(userContext(), "t1"); err != nil || c.stops.Load() != 1 {
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

	if _, err := r.svc.Resume(userContext(), "t1"); err == nil {
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

	if err := r.svc.AnswerDecision(userContext(), "auth1", ans); err == nil {
		t.Fatal("the answer resumed with a failing stop")
	}
	d, _ := r.load().Decision("auth1")
	if d.Status != domain.DecisionOpen || r.runState() != domain.RunPaused {
		t.Fatalf("decision %s, run %s; want both as they were", d.Status, r.runState())
	}
	c.stopErr = nil
	r.agent.Block()
	must(t, r.svc.AnswerDecision(userContext(), "auth1", ans))
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

// restart makes the service forget which environments it started, as a new
// supervisor process does, and counts the stops and starts from here on. The
// environments and their runs are as the old process left them.
func (r *wsRig) restart() *countingRuntime {
	r.t.Helper()
	r.svc.mu.Lock()
	r.svc.startedEnvs = nil
	r.svc.mu.Unlock()
	c := &countingRuntime{Adapter: r.svc.rt}
	r.svc.rt = c
	return c
}

// interruptWithLogin leaves the task's run interrupted with an open login
// question and its session gone, as a killed supervisor leaves it.
func (r *wsRig) interruptWithLogin(task, run, decision domain.ID) {
	r.t.Helper()
	must(r.t, r.svc.update(bg, task, func(a *domain.TaskAggregate) error {
		_, err := a.SuspendRun(run, domain.CauseAuthExpired, time.Time{}, decision, r.clock.now)
		return err
	}))
	r.svc.stopSession(run)
	r.svc.Wait()
	must(r.t, r.svc.update(bg, task, func(a *domain.TaskAggregate) error { return a.Interrupt(run) }))
}

// Finding E of the re-review of #201: run I is interrupted with an open login
// question, the supervisor is killed, and in the new process the human starts
// another task in the same workspace and then answers I's question. The second
// task's agent must not be killed by I's relaunch: the start is refused, because
// the interrupted run still owns the environment.
func TestAnInterruptedRunKeepsAnotherTaskOutOfItsEnvironment(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	_, first := r.create("busy")
	second, err := r.ws.AddAgent(bg, "busy", "runtime", "", "")
	must(t, err)
	task1, run1, err := r.ws.StartTask(userContext(), StartRequest{AgentID: first.ID, Issue: "#1"})
	must(t, err)
	r.interruptWithLogin(task1, run1, "login1")
	c := r.restart()

	_, _, err = r.ws.StartTask(userContext(), StartRequest{AgentID: second.ID, Issue: "#2"})
	var conf *domain.ConflictError
	if !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("starting task 2: %v; want environment busy by the interrupted run", err)
	}
	if ids, _ := r.store.ActiveTaskIDs(bg); len(ids) != 1 {
		t.Errorf("the refused start left a task: %v", ids)
	}
	if c.stops.Load() != 0 {
		t.Errorf("a refused start stopped the environment %d times", c.stops.Load())
	}

	// I's answer resumes it after one stop and start; nothing else lives there.
	ans := domain.Response{By: "w", Option: domain.AnswerResume, At: r.clock.now}
	r.agent.Block() // the first run's block was used by its launch; an unscripted relaunch finishes at once and stops the run
	must(t, r.svc.AnswerDecision(userContext(), "login1", ans))
	run, _ := mustRun(t, r.store, task1, run1)
	if run.State != domain.RunRunning || !r.svc.attached(run1) || c.stops.Load() != 1 || c.starts.Load() != 1 {
		t.Errorf("run %s, attached %v, stops %d, starts %d", run.State, r.svc.attached(run1), c.stops.Load(), c.starts.Load())
	}
}

func mustRun(t *testing.T, s *store.Store, task, run domain.ID) (domain.Run, bool) {
	t.Helper()
	a, err := s.LoadTask(bg, task)
	must(t, err)
	return a.Run(run)
}

// Every path to starting checks that no other run owns the environment: here a
// run of another task does (a record from before the rule), and the resume, the
// answer that resumes and the reconciler all leave the interrupted run alone and
// stop nothing.
func TestResumePathsRefuseWhileAnotherRunOwnsTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	ws, first := r.create("owned")
	second, err := r.ws.AddAgent(bg, "owned", "runtime", "", "")
	must(t, err)
	task1, run1, err := r.ws.StartTask(userContext(), StartRequest{AgentID: first.ID, Issue: "#1"})
	must(t, err)
	r.interruptWithLogin(task1, run1, "login1")
	// A run of another task in the same environment, saved directly.
	agg := domain.NewTaskAggregate(domain.Task{ID: "t2", Repo: ws.Repo, Issue: "#2", State: domain.TaskQueued, AgentID: second.ID, CreatedAt: r.clock.now})
	agg.AddEnvironment(domain.Environment{ID: ws.EnvID, Backend: r.svc.rt.Name(), State: domain.EnvRunning})
	must(t, agg.StartRun(domain.Run{ID: "r2", WorkspaceID: ws.ID, AgentID: second.ID, EnvID: ws.EnvID}))
	_, err = r.store.SaveTask(bg, agg)
	must(t, err)
	c := r.restart()

	ans := domain.Response{By: "w", Option: domain.AnswerResume, At: r.clock.now}
	var conf *domain.ConflictError
	if err := r.svc.AnswerDecision(userContext(), "login1", ans); !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Errorf("the answer: %v", err)
	}
	if _, err := r.svc.Resume(userContext(), task1); err == nil {
		t.Error("Resume succeeded while another run owns the environment")
	}
	if run, _ := mustRun(t, r.store, task1, run1); run.State != domain.RunInterrupted || c.stops.Load() != 0 {
		t.Errorf("run %s, stops %d; want it interrupted and nothing stopped", run.State, c.stops.Load())
	}
}

// A new run's start in an environment this process did not start stops and
// starts it once before the agent is launched; later starts, passes and resumes
// do not stop it again. A failed stop fails the start and launches nothing.
func TestANewRunsStartStopsAndStartsAnEnvironmentThisProcessDidNotStart(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	_, a := r.create("fresh")
	task, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#1"})
	must(t, err)
	must(t, r.svc.Cancel(bg, task))
	r.svc.Wait()

	c := r.restart()
	launched := len(r.agent.Specs)
	c.stopErr = errors.New("stop refused")
	if _, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#2"}); !errors.Is(err, errStopFailed) {
		t.Fatalf("start with a failing stop: %v", err)
	}
	if len(r.agent.Specs) != launched || c.starts.Load() != 0 {
		t.Errorf("a failed stop launched %d agents and started the environment %d times", len(r.agent.Specs)-launched, c.starts.Load())
	}
	if ids, _ := r.store.ActiveTaskIDs(bg); len(ids) != 0 {
		t.Errorf("the failed start left a task: %v", ids)
	}

	c.stopErr = nil
	r.agent.Block() // each launch below gets its own blocking session
	task2, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#3"})
	must(t, err)
	if c.stops.Load() != 2 || c.starts.Load() != 1 || len(r.agent.Specs) != launched+1 {
		t.Errorf("stops %d (one failed), starts %d, launches %d", c.stops.Load(), c.starts.Load(), len(r.agent.Specs)-launched)
	}
	// A pass and a retry-style new run later do not stop it again.
	_, _ = r.svc.Reconcile(bg)
	must(t, r.svc.Cancel(bg, task2))
	r.svc.Wait()
	r.agent.Block()
	if _, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#4"}); err != nil {
		t.Fatal(err)
	}
	if c.stops.Load() != 2 {
		t.Errorf("stops %d after later starts; want no further stop", c.stops.Load())
	}
}

// The reconciler's recovery is held to the same rule: an interrupted run with no
// open question is not relaunched while another run owns the environment.
func TestRecoveryRefusesWhileAnotherRunOwnsTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	ws, first := r.create("recover")
	second, err := r.ws.AddAgent(bg, "recover", "runtime", "", "")
	must(t, err)
	task1, run1, err := r.ws.StartTask(userContext(), StartRequest{AgentID: first.ID, Issue: "#1"})
	must(t, err)
	r.svc.stopSession(run1)
	r.svc.Wait()
	agg := domain.NewTaskAggregate(domain.Task{ID: "t2", Repo: ws.Repo, Issue: "#2", State: domain.TaskQueued, AgentID: second.ID, CreatedAt: r.clock.now})
	agg.AddEnvironment(domain.Environment{ID: ws.EnvID, Backend: r.svc.rt.Name(), State: domain.EnvRunning})
	must(t, agg.StartRun(domain.Run{ID: "r2", WorkspaceID: ws.ID, AgentID: second.ID, EnvID: ws.EnvID}))
	_, err = r.store.SaveTask(bg, agg)
	must(t, err)
	c := r.restart()

	var conf *domain.ConflictError
	if err := r.svc.recover(userContext(), task1, run1, &Report{}); !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Errorf("recovery: %v", err)
	}
	if run, _ := mustRun(t, r.store, task1, run1); run.State != domain.RunInterrupted || c.stops.Load() != 0 {
		t.Errorf("run %s, stops %d", run.State, c.stops.Load())
	}
}

// A failed read at the resume gate fails the resume and the answer: nothing is
// stopped, started or launched on a state this process could not read (#216).
func TestAFailedReadAtTheResumeGateLaunchesNothing(t *testing.T) {
	t.Parallel()
	boom := errors.New("read failed")
	failFirst := func(r *rig) {
		orig := r.svc.loadTask
		n := 0
		r.svc.loadTask = func(ctx context.Context, id domain.ID) (*domain.TaskAggregate, error) {
			if n++; n == 1 {
				return nil, boom
			}
			return orig(ctx, id)
		}
	}
	r := newRig(t)
	r.pauseStored()
	c := r.counting()
	failFirst(r)
	launched := len(r.agent.Specs)
	if _, err := r.svc.Resume(userContext(), "t1"); !errors.Is(err, boom) {
		t.Fatalf("resume: %v", err)
	}
	if r.runState() != domain.RunPaused || len(r.agent.Specs) != launched || c.stops.Load() != 0 || c.starts.Load() != 0 {
		t.Errorf("run %s, stops %d, starts %d", r.runState(), c.stops.Load(), c.starts.Load())
	}

	r2 := newRig(t)
	a := r2.load()
	_, err := a.SuspendRun("r1", domain.CauseAuthExpired, time.Time{}, "auth1", r2.clock.now)
	must(t, err)
	_, err = r2.store.SaveTask(bg, a)
	must(t, err)
	c2 := r2.counting()
	r2.svc.markEnvStarted(r2.env) // freshenForResume loads once and returns early
	n := 0
	orig := r2.svc.loadTask
	r2.svc.loadTask = func(ctx context.Context, id domain.ID) (*domain.TaskAggregate, error) {
		if n++; n == 2 { // the gate's own read, after freshenForResume's
			return nil, boom
		}
		return orig(ctx, id)
	}
	ans := domain.Response{By: "w", Option: domain.AnswerResume, At: r2.clock.now}
	if err := r2.svc.AnswerDecision(userContext(), "auth1", ans); !errors.Is(err, boom) {
		t.Fatalf("answer: %v", err)
	}
	if d, _ := r2.load().Decision("auth1"); d.Status != domain.DecisionOpen || r2.runState() != domain.RunPaused || c2.starts.Load() != 0 {
		t.Errorf("decision %s, run %s", d.Status, r2.runState())
	}
}

// A cancel whose stop fails is saved, says so, and is retried by the next
// reconciler pass until the stop works.
func TestACancelWhoseStopFailsIsRetriedByTheReconciler(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.pauseStored()
	c := r.counting()
	c.stopErr = errors.New("stop refused")
	err := r.svc.Cancel(bg, "t1")
	if err == nil || !strings.Contains(err.Error(), "is cancelled") || !errors.Is(err, errStopFailed) {
		t.Fatalf("cancel: %v", err)
	}
	if r.load().Task().State != domain.TaskCancelled {
		t.Fatalf("task is %s", r.load().Task().State)
	}
	if rep, _ := r.svc.Reconcile(bg); len(rep.Errors) == 0 {
		t.Error("a failing retry reported nothing")
	}
	c.stopErr = nil
	before := c.stops.Load()
	rep, rerr := r.svc.Reconcile(bg)
	must(t, rerr)
	if len(rep.Errors) != 0 || c.stops.Load() != before+1 || r.envState() != domain.EnvStopped {
		t.Errorf("errors %v, stops %d, env %s", rep.Errors, c.stops.Load()-before, r.envState())
	}
	must(t, func() error { _, e := r.svc.Reconcile(bg); return e }())
	if c.stops.Load() != before+1 {
		t.Error("a stopped environment was stopped again")
	}
}

// failStartRuntime fails Start while fail is set.
type failStartRuntime struct {
	runtime.Adapter
	fail atomic.Bool
}

func (f *failStartRuntime) Start(ctx context.Context, id string) error {
	if f.fail.Load() {
		return errors.New("start refused")
	}
	return f.Adapter.Start(ctx, id)
}

// A failed start takes the mark back only when this call set it: a mark set by
// an earlier successful start stays, also under concurrent failing starts
// (issue #221).
func TestAFailedStartKeepsAMarkAnEarlierStartSet(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	f := &failStartRuntime{Adapter: r.rt.Adapter}
	r.svc.rt = f

	f.fail.Store(true)
	if err := r.svc.startEnv(bg, string(r.env)); err == nil || r.svc.envStarted(r.env) {
		t.Fatalf("a failed first start: err %v, marked %v; want the mark taken back", err, r.svc.envStarted(r.env))
	}
	f.fail.Store(false)
	if err := r.svc.startEnv(bg, string(r.env)); err != nil {
		t.Fatal(err)
	}
	f.fail.Store(true)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _ = r.svc.startEnv(bg, string(r.env)) })
	}
	wg.Wait()
	if !r.svc.envStarted(r.env) {
		t.Error("a failed start forgot the mark an earlier start set")
	}
}

// Two supervisors of one owner, each with its own store, share one runtime: the
// sweep of each stops only the environments its own store records (issue #221).
func TestTheSweepLeavesAnotherSupervisorsEnvironmentsAlone(t *testing.T) {
	t.Parallel()
	a := newRig(t) // service A: its store records a.env
	stB, err := store.Open(bg, filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stB.Close() })
	// A second environment in A's runtime, recorded only in a second store.
	prep, err := a.rt.Prepare(a.rt.NewSpec())
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.rt.Adapter.Provision(bg, prep)
	if err != nil {
		t.Fatal(err)
	}
	must(t, a.rt.Adapter.Start(bg, other))
	agg := domain.NewTaskAggregate(domain.Task{ID: "t9", Repo: "wstein/workharbor", Issue: "#9", State: domain.TaskRunning, CreatedAt: t0})
	agg.AddEnvironment(domain.Environment{ID: domain.ID(other), Backend: "fake", State: domain.EnvRunning})
	if _, err := stB.SaveTask(bg, agg); err != nil {
		t.Fatal(err)
	}
	c := &countingRuntime{Adapter: a.rt.Adapter}
	svcB := New(stB, c, a.agent, a.clock, Config{Owner: a.rt.Owner, ReadyCmd: []string{"echo", "ready"}})
	t.Cleanup(svcB.Shutdown)

	// B's sweep: a.env is not in B's store, so B must not stop it.
	infos, err := c.List(bg, a.rt.Owner)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[domain.ID]runtime.Info{}
	for _, in := range infos {
		seen[domain.ID(in.ID)] = in
	}
	if errs := svcB.stopLeftoverEnvs(bg, seen); len(errs) != 0 {
		t.Fatal(errs)
	}
	if c.stops.Load() != 1 {
		t.Fatalf("B stopped %d environments, want 1 (its own)", c.stops.Load())
	}
	if in, err := a.rt.Adapter.Inspect(bg, string(a.env)); err != nil || in.State != domain.EnvRunning {
		t.Errorf("A's environment: %v, %v; B must leave it running", in.State, err)
	}
	if in, err := a.rt.Adapter.Inspect(bg, other); err != nil || in.State != domain.EnvStopped {
		t.Errorf("B's recorded environment: %v, %v; want stopped", in.State, err)
	}
	// A recorded environment this process did not start is stopped once per pass.
	infos, _ = c.List(bg, a.rt.Owner)
	seen = map[domain.ID]runtime.Info{}
	for _, in := range infos {
		seen[domain.ID(in.ID)] = in
	}
	svcB.stopLeftoverEnvs(bg, seen)
	if c.stops.Load() != 1 {
		t.Errorf("stops %d after a pass with nothing running of B's", c.stops.Load())
	}
}
