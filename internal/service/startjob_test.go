package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
)

// withBlockingPostCreate gives the rig a repository whose post-create command
// blocks until the context ends (the fake's sleep), and a short StartWait.
func (r *wsRig) withBlockingPostCreate() {
	r.svc.cfg.StartWait = 50 * time.Millisecond
	r.ws.cfg.Environment = repoEnv(devcontainer.OriginDevcontainer, devcontainer.Config{
		Image: "docker.io/library/golang:1.27.1", PostCreate: []devcontainer.Command{{Argv: []string{"sleep"}}},
	}, nil)
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) == 3 && cmd[0] == "sh" && strings.HasPrefix(cmd[2], "test -f ") {
			return nil, "", 1, true
		}
		return nil, "", 0, false // sleep blocks until its context ends
	}
}

func (r *wsRig) onlyTask() (domain.ID, domain.ID) {
	r.t.Helper()
	list, _ := r.svc.List(bg, false)
	if len(list) != 1 {
		r.t.Fatalf("tasks = %+v", list)
	}
	agg, _ := r.store.LoadTask(bg, list[0].ID)
	runs := agg.Runs()
	if len(runs) != 1 {
		r.t.Fatalf("runs = %+v", runs)
	}
	return list[0].ID, runs[0].ID
}

// A slow post-create does not hold the request: StartTask returns with the run
// starting, and Cancel stops the command that is already running (finding 10).
func TestCancelStopsAPostCreateThatIsRunning(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.withBlockingPostCreate()
	_, a := r.create("docs-ws")
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"}); err != nil {
		t.Fatal(err)
	}
	task, run := r.onlyTask()
	agg, _ := r.store.LoadTask(bg, task)
	if rn, _ := agg.Run(run); rn.State != domain.RunStarting || r.agent.Started() != 0 {
		t.Fatalf("run %s, agents %d: the run is starting while post-create runs", rn.State, r.agent.Started())
	}
	if r.svc.startDoneChan(run) == nil {
		t.Fatal("no start is in progress")
	}
	if err := r.svc.Cancel(bg, task); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.svc.startDoneChan(run) == nil })
	if r.svc.startDoneChan(run) != nil {
		t.Fatal("the cancelled post-create is still running")
	}
	if r.agent.Started() != 0 || r.svc.attached(run) {
		t.Errorf("a cancelled start started an agent (%d) or kept its slot", r.agent.Started())
	}
	agg, _ = r.store.LoadTask(bg, task)
	if rn, _ := agg.Run(run); rn.State != domain.RunStopped {
		t.Errorf("run = %s, want stopped, not failed: the human cancelled it", rn.State)
	}
}

// A post-create that does not finish fails the run with a clear reason (finding 9).
func TestAPostCreateThatDoesNotFinishFailsTheRun(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.withBlockingPostCreate()
	r.svc.cfg.PostCreateTimeout = 100 * time.Millisecond
	_, a := r.create("docs-ws")
	_, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"})
	// The request waited only 50 ms, so the start is still running; it ends in a failure.
	if err != nil {
		t.Logf("the start failed within the wait: %v", err)
	}
	task, run := r.onlyTask()
	eventually(t, func() bool {
		agg, _ := r.store.LoadTask(bg, task)
		rn, _ := agg.Run(run)
		return rn.State == domain.RunFailed
	})
	agg, _ := r.store.LoadTask(bg, task)
	if rn, _ := agg.Run(run); rn.State != domain.RunFailed {
		t.Fatalf("run = %s, want failed", rn.State)
	}
	if r.agent.Started() != 0 {
		t.Error("the agent started after a post-create that timed out")
	}
	var said bool
	eventually(t, func() bool {
		for _, e := range r.reported() {
			if strings.Contains(e.Error(), "did not finish within 100ms") {
				said = true
			}
		}
		return said || err != nil
	})
	if !said && (err == nil || !strings.Contains(err.Error(), "did not finish within 100ms")) {
		t.Errorf("the reason is not clear: reported %v, returned %v", r.reported(), err)
	}
}

// An answer that arrives on a request which then ends does not fail the run
// whose answer was stored: the start runs on its own context (finding 9).
func TestTheStartOutlivesTheRequestThatAnsweredTheLastEgressRequest(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.withEgressRequests()
	_, a := r.create("docs-ws")
	release := make(chan struct{})
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"})
	if err != nil {
		t.Fatal(err)
	}
	// Hold the agent's start: the allowlist update runs first, and the fake agent starts at once,
	// so the request's context is cancelled the moment AnswerDecision returns.
	open := r.openEgress(task)
	ctx, cancel := context.WithCancel(bg)
	var first bool
	for _, d := range open {
		if err := r.svc.AnswerDecision(ctx, d.ID, domain.Response{Option: domain.AnswerAllow, By: "werner", At: t0}); err != nil {
			t.Fatal(err)
		}
		first = true
	}
	cancel() // the phone's request is gone
	close(release)
	if !first {
		t.Fatal("no request was open")
	}
	eventually(t, func() bool {
		agg, _ := r.store.LoadTask(bg, task)
		rn, _ := agg.Run(run)
		return rn.State == domain.RunRunning
	})
	agg, _ := r.store.LoadTask(bg, task)
	if rn, _ := agg.Run(run); rn.State != domain.RunRunning || r.agent.Started() != 1 {
		t.Errorf("run %s, agents %d: a dropped request failed a run whose answers were stored", rn.State, r.agent.Started())
	}
}

// At most one start runs per run, and a second call gets the first one's job.
func TestAtMostOneStartPerRun(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	var calls atomic.Int32
	release := make(chan struct{})
	fn := func(ctx context.Context) error {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}
	first := r.svc.startDetached(bg, "r1", fn)
	second := r.svc.startDetached(bg, "r1", fn)
	if first == nil || first != second {
		t.Fatalf("jobs %p %p: a second start of the run must return the running one", first, second)
	}
	close(release)
	<-first.done
	if calls.Load() != 1 {
		t.Errorf("the start ran %d times", calls.Load())
	}
	// Once it has ended, a new start is allowed.
	if r.svc.startDetached(bg, "r1", func(context.Context) error { return nil }) == nil {
		t.Error("a start after the first ended was refused")
	}
}

// Shutdown cancels a start that is running and waits for it.
func TestShutdownCancelsARunningStart(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	started := make(chan struct{})
	var ended atomic.Bool
	r.svc.startDetached(bg, "r1", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		ended.Store(true)
		return ctx.Err()
	})
	<-started
	r.svc.Shutdown()
	if !ended.Load() {
		t.Error("Shutdown returned before the start ended")
	}
	if r.svc.startDetached(bg, "r2", func(context.Context) error { return nil }) != nil {
		t.Error("a start began after Shutdown")
	}
}

// A Cancel that commits after the agent started and the run was marked running,
// and before the session is attached, must not leave the agent running for a
// cancelled task: the start's context is cancelled, so the session is stopped,
// once. Two paths end it (the ctx check before attach, and attach honouring the
// slot's stop request), so this test does not tell them apart: it guards the
// outcome, and the check before attach only saves attaching a session that is
// to be stopped at once.
func TestACancelBetweenMarkRunningAndAttachStopsTheAgent(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	_, a := r.create("docs-ws")
	var task domain.ID
	r.svc.testBeforeAttach = func(domain.ID) {
		list, _ := r.svc.List(bg, false)
		task = list[0].ID
		if err := r.svc.Cancel(bg, task); err != nil {
			t.Errorf("cancel: %v", err)
		}
	}
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"}); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	list, _ := r.svc.List(bg, false)
	agg, _ := r.store.LoadTask(bg, list[0].ID)
	run := agg.Runs()[0].ID
	eventually(t, func() bool { return !r.svc.attached(run) })
	if r.svc.attached(run) {
		t.Fatal("the agent of a cancelled task is still attached")
	}
	if agg.Task().State != domain.TaskCancelled || agg.Runs()[0].State != domain.RunStopped {
		t.Errorf("task %s, run %s", agg.Task().State, agg.Runs()[0].State)
	}
	if n := r.agent.Stops(); n != 1 {
		t.Errorf("the session was stopped %d times, want once", n)
	}
}

// The same interleaving when only the slot's stop request, not the start's
// context, is set: the session is stopped as soon as it is attached.
func TestAStopRequestedBeforeAttachStopsTheSession(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	_, a := r.create("docs-ws")
	r.svc.testBeforeAttach = func(run domain.ID) { r.svc.stopSession(run) }
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"}); err != nil {
		t.Fatal(err)
	}
	list, _ := r.svc.List(bg, false)
	agg, _ := r.store.LoadTask(bg, list[0].ID)
	run := agg.Runs()[0].ID
	// The fake agent was told to block; the stop request ends it, and with it the slot.
	eventually(t, func() bool { return !r.svc.attached(run) })
	if r.svc.attached(run) {
		t.Fatal("a session that was asked to stop while it was starting is still attached")
	}
	if n := r.agent.Stops(); n != 1 {
		t.Errorf("the session was stopped %d times, want once", n)
	}
}
