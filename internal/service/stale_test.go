package service

import (
	"context"
	"errors"
	"strings"
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
	task1, run1, err := r.ws.StartTask(bg, StartRequest{AgentID: first.ID, Issue: "#1"})
	must(t, err)
	r.interruptWithLogin(task1, run1, "login1")
	c := r.restart()

	_, _, err = r.ws.StartTask(bg, StartRequest{AgentID: second.ID, Issue: "#2"})
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
	must(t, r.svc.AnswerDecision(bg, "login1", ans))
	run, _ := mustRun(t, r.store, task1, run1)
	if run.State != domain.RunRunning || c.stops.Load() != 1 || c.starts.Load() != 1 {
		t.Errorf("run %s, stops %d, starts %d", run.State, c.stops.Load(), c.starts.Load())
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
	task1, run1, err := r.ws.StartTask(bg, StartRequest{AgentID: first.ID, Issue: "#1"})
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
	if err := r.svc.AnswerDecision(bg, "login1", ans); !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Errorf("the answer: %v", err)
	}
	if _, err := r.svc.Resume(bg, task1); err == nil {
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
	task, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"})
	must(t, err)
	must(t, r.svc.Cancel(bg, task))
	r.svc.Wait()

	c := r.restart()
	launched := len(r.agent.Specs)
	c.stopErr = errors.New("stop refused")
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#2"}); !errors.Is(err, errStopFailed) {
		t.Fatalf("start with a failing stop: %v", err)
	}
	if len(r.agent.Specs) != launched || c.starts.Load() != 0 {
		t.Errorf("a failed stop launched %d agents and started the environment %d times", len(r.agent.Specs)-launched, c.starts.Load())
	}
	if ids, _ := r.store.ActiveTaskIDs(bg); len(ids) != 0 {
		t.Errorf("the failed start left a task: %v", ids)
	}

	c.stopErr = nil
	task2, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#3"})
	must(t, err)
	if c.stops.Load() != 2 || c.starts.Load() != 1 || len(r.agent.Specs) != launched+1 {
		t.Errorf("stops %d (one failed), starts %d, launches %d", c.stops.Load(), c.starts.Load(), len(r.agent.Specs)-launched)
	}
	// A pass and a retry-style new run later do not stop it again.
	_, _ = r.svc.Reconcile(bg)
	must(t, r.svc.Cancel(bg, task2))
	r.svc.Wait()
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#4"}); err != nil {
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
	task1, run1, err := r.ws.StartTask(bg, StartRequest{AgentID: first.ID, Issue: "#1"})
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
	if err := r.svc.recover(bg, task1, run1, &Report{}); !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Errorf("recovery: %v", err)
	}
	if run, _ := mustRun(t, r.store, task1, run1); run.State != domain.RunInterrupted || c.stops.Load() != 0 {
		t.Errorf("run %s, stops %d", run.State, c.stops.Load())
	}
}
