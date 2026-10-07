package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/store"
)

func TestLateUsageCannotStopSuccessorRun(t *testing.T) {
	r := newRig(t)
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error {
		if err := a.StopRun("r1"); err != nil {
			return err
		}
		return a.StartRun(domain.Run{ID: "r2", EnvID: r.env})
	}))
	r.svc.cfg.Budgets = Budgets{PerTask: Limit{MaxTokens: 1}}
	r.turn(1, 2, 0)
	v, err := r.svc.Show(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Task.State == domain.TaskFailed || v.Runs[1].State.Terminal() {
		t.Fatal("late r1 usage stopped its successor")
	}
}

func TestNoUsageIsUnknown(t *testing.T) {
	if got := FormatUsageLine(UsageReport{}); !strings.Contains(got, "unknown") {
		t.Fatalf("no reports = %q, want unknown", got)
	}
}

func TestPausedDurationExpiresWithoutUsage(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: 10 * time.Second}}
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error { return a.Pause("r1") }))
	r.clock.now = t0.Add(10 * time.Second)
	r.svc.checkBudgets(bg, "t1", "r1")
	v, err := r.svc.Show(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Task.State != domain.TaskFailed {
		t.Fatalf("paused task = %s", v.Task.State)
	}
	b := r.auditKinds(domain.EventBudgetExceeded)
	if len(b) != 1 || b[0].Metric != domain.BudgetDuration || b[0].Used != 10000 {
		t.Fatalf("breach = %+v", b)
	}
}

func TestDurationSoftWarningIsAtomicAndHardTakesPrecedence(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: 10 * time.Second}}
	r.clock.now = t0.Add(8 * time.Second)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); r.svc.checkBudgets(bg, "t1", "r1") }()
	}
	wg.Wait()
	if got := len(r.auditKinds(domain.EventBudgetWarned)); got != 1 {
		t.Fatalf("soft warnings = %d", got)
	}
	r.clock.now = t0.Add(10 * time.Second)
	r.svc.checkBudgets(bg, "t1", "r1")
	r.svc.checkBudgets(bg, "t1", "r1")
	a := r.load()
	run, _ := a.Run("r1")
	if run.TerminalReason != "budget_breach" || len(r.auditKinds(domain.EventBudgetExceeded)) != 1 {
		t.Fatalf("terminal run=%+v", run)
	}
}

func TestDurationRestartCatchesUpBeforeResumeAndRollbackRefuses(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "downtime", true: "rollback"}[rollback], func(t *testing.T) {
			r := newRig(t)
			r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: 10 * time.Second}}
			r.clock.now = t0.Add(8 * time.Second)
			r.svc.checkBudgets(bg, "t1", "r1")
			// A fresh service loses all in-process anchors while retaining the database.
			fresh := New(r.store, r.rt.Adapter, r.agent, r.clock, r.svc.cfg)
			must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error { return a.Interrupt("r1") }))
			if rollback {
				r.clock.now = t0.Add(7 * time.Second)
			} else {
				r.clock.now = t0.Add(12 * time.Second)
			}
			if err := fresh.durationAdmission(bg, "t1", "r1"); err == nil {
				t.Fatal("unsafe resume permitted")
			}
			a := r.load()
			run, _ := a.Run("r1")
			if rollback {
				if run.DurationMillis < 8000 || run.State.Terminal() {
					t.Fatalf("rollback lost accounting=%+v", run)
				}
			} else if a.Task().State != domain.TaskFailed || run.DurationMillis != 12000 {
				t.Fatalf("restart=%+v", run)
			}
		})
	}
}

func TestTaskDurationSumsRunsButExcludesQueueAndIdle(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerTask: Limit{MaxDuration: 10 * time.Second}}
	r.clock.now = t0.Add(4 * time.Second)
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error { return a.StopRun("r1") }))
	r.clock.now = t0.Add(time.Hour)
	must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error { return a.StartRun(domain.Run{ID: "r2", EnvID: r.env}) }))
	r.svc.checkBudgets(bg, "t1", "r2")
	if r.load().Task().State == domain.TaskFailed {
		t.Fatal("idle time consumed task lifetime")
	}
	r.clock.now = r.clock.now.Add(6 * time.Second)
	r.svc.checkBudgets(bg, "t1", "r2")
	b := r.auditKinds(domain.EventBudgetExceeded)
	if len(b) != 1 || b[0].Scope != domain.BudgetTask || b[0].Used != 10000 {
		t.Fatalf("task duration=%+v", b)
	}
}

func TestInitialAdmissionRefusesExpiredPreparation(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: time.Second}}
	r.clock.now = t0.Add(time.Second)
	if err := r.svc.durationAdmission(bg, "t1", "r1"); err == nil {
		t.Fatal("expired admission executed")
	}
	if r.load().Task().State != domain.TaskFailed {
		t.Fatal("expired admission did not fail task")
	}
}

func TestTerminalReasonSurvivesLateCompletionAndUsage(t *testing.T) {
	r := newRig(t)
	must(t, r.svc.Cancel(bg, "t1"))
	must(t, r.svc.finish(bg, "t1", "r1", agent.Result{Status: agent.ResultCompleted}))
	r.turn(1, 99, 99)
	run, _ := r.load().Run("r1")
	if run.TerminalReason != "human_cancellation" {
		t.Fatalf("reason=%q", run.TerminalReason)
	}
}

type blockedDurationRuntime struct {
	runtime.Adapter
	listed chan struct{}
}

func (r *blockedDurationRuntime) List(ctx context.Context, _ string) ([]runtime.Info, error) {
	close(r.listed)
	<-ctx.Done()
	return nil, ctx.Err()
}

type durationTestClock struct {
	SystemClock
	nanos atomic.Int64
}

func (c *durationTestClock) Now() time.Time { return time.Unix(0, c.nanos.Load()) }

func TestDurationScheduleRunsWhileReconciliationIsBlocked(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: time.Second}}
	clock := &durationTestClock{}
	clock.nanos.Store(t0.UnixNano())
	r.svc.clock = clock
	rt := &blockedDurationRuntime{Adapter: r.rt.Adapter, listed: make(chan struct{})}
	r.svc.rt = rt
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	reconciled := make(chan struct{})
	go func() { defer close(reconciled); _, _ = r.svc.Reconcile(ctx) }()
	<-rt.listed
	clock.nanos.Store(t0.Add(time.Second).UnixNano())
	checked := make(chan struct{})
	go func() { defer close(checked); r.svc.RunDurationBudgets(ctx) }()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for r.load().Task().State != domain.TaskFailed {
		select {
		case <-poll.C:
		case <-deadline.C:
			cancel()
			<-checked
			<-reconciled
			t.Fatal("blocked reconciler suppressed duration enforcement")
		}
	}
	ended, _ := r.load().Run("r1")
	if !ended.EndedAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("terminal bound ignored injected supervisor clock: %v", ended.EndedAt)
	}
	cancel()
	<-checked
	<-reconciled
}

func TestUnknownZeroAndPartialUsageAreDistinct(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  store.UsageRow
		want string
	}{
		{"unknown", store.UsageRow{Turns: 2, TurnsWithoutToken: 2, TurnsWithoutCost: 2}, "tokens unknown"},
		{"zero", store.UsageRow{Turns: 1}, "0 in, 0 out"},
		{"partial", store.UsageRow{Turns: 2, TurnsWithoutToken: 1, TurnsWithoutCost: 1}, "partial; 1 turns without cost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatUsageLine(UsageReport{Rows: []UsageRow{{UsageRow: tc.row}}})
			if !strings.Contains(got, tc.want) {
				t.Fatalf("%s: %q", tc.name, got)
			}
		})
	}
}

func TestDurationCheckKeepsOwnershipWithoutBlockingOtherRuns(t *testing.T) {
	r := newRig(t)
	r.live()
	r.svc.mu.Lock()
	sl := r.svc.sessions["r1"]
	gate := &gateSession{Session: sl.sess, entered: make(chan struct{}), gate: make(chan struct{})}
	sl.sess = gate
	r.svc.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(gate.gate) }) }
	t.Cleanup(release)
	second := domain.NewTaskAggregate(domain.Task{ID: "t2", Repo: "wstein/workharbor", State: domain.TaskRunning, CreatedAt: t0})
	second.AddEnvironment(domain.Environment{ID: "e2", Backend: "fake", State: domain.EnvRunning})
	must(t, second.StartRun(domain.Run{ID: "r2", EnvID: "e2"}))
	_, err := r.store.SaveTask(bg, second)
	must(t, err)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: time.Second}}
	r.clock.now = t0.Add(time.Second)
	if errs := r.svc.scheduleDurationChecks(bg); len(errs) > 0 {
		t.Fatal(errs)
	}
	select {
	case <-gate.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("budget cleanup never started")
	}
	pollUntil(t, func() bool {
		a, err := r.store.LoadTask(bg, "t2")
		return err == nil && a.Task().State == domain.TaskFailed
	})
	if err := r.svc.checkEnvFree(bg, r.env, ""); err == nil {
		t.Fatal("terminal run released environment before cleanup")
	}
	release()
	r.svc.Wait()
}

func TestSimultaneousHardBudgetsUseDeterministicScopeAndMetric(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  Limit
		metric domain.BudgetMetric
		scope  domain.BudgetScope
	}{
		{"run tokens", Limit{MaxTokens: 2, MaxCostMicroUSD: 2, MaxDuration: 10 * time.Second}, domain.BudgetTokens, domain.BudgetRun},
		{"run cost", Limit{MaxCostMicroUSD: 2, MaxDuration: 10 * time.Second}, domain.BudgetCost, domain.BudgetRun},
		{"run duration before task tokens", Limit{MaxDuration: 10 * time.Second}, domain.BudgetDuration, domain.BudgetRun},
		{"task tokens", Limit{}, domain.BudgetTokens, domain.BudgetTask},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.svc.cfg.Budgets = Budgets{PerRun: tc.limit, PerTask: Limit{MaxTokens: 2, MaxCostMicroUSD: 2, MaxDuration: 10 * time.Second}}
			r.clock.now = t0.Add(10 * time.Second)
			r.turn(1, 2, 2)
			b := r.auditKinds(domain.EventBudgetExceeded)
			if len(b) != 1 || b[0].Scope != tc.scope || b[0].Metric != tc.metric {
				t.Fatalf("hard precedence=%+v", b)
			}
			if len(r.auditKinds(domain.EventBudgetWarned)) != 0 {
				t.Fatal("warning recorded beside simultaneous hard breach")
			}
		})
	}
}

// The command observes cancellation but deliberately delays returning, like a
// runtime finishing cleanup after its process was asked to stop.
type durationPreparationGate struct {
	runtime.Adapter
	entered, cancelled, release chan struct{}
}

func (g *durationPreparationGate) Exec(ctx context.Context, env string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if len(req.Cmd) == 1 && req.Cmd[0] == "sleep" {
		close(g.entered)
		<-ctx.Done()
		close(g.cancelled)
		<-g.release
		return nil, ctx.Err()
	}
	return g.Adapter.Exec(ctx, env, req)
}

func TestDurationExpiryCancelsPreparationAndHoldsUntilCleanup(t *testing.T) {
	r := newWsRig(t)
	r.withBlockingPostCreate()
	gate := &durationPreparationGate{Adapter: r.svc.rt, entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	r.svc.rt = gate
	var once sync.Once
	release := func() { once.Do(func() { close(gate.release) }) }
	t.Cleanup(func() {
		r.svc.mu.Lock()
		for _, job := range r.svc.starts {
			job.cancel()
		}
		r.svc.mu.Unlock()
		release()
	})
	clock := &durationTestClock{}
	clock.nanos.Store(t0.UnixNano())
	r.svc.clock = clock
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: time.Second}}
	w, a := r.create("duration-start")
	_, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#1"})
	must(t, err)
	task, run := r.onlyTask()
	<-gate.entered
	clock.nanos.Store(t0.Add(time.Second).UnixNano())
	r.svc.checkBudgets(bg, task, run)
	select {
	case <-gate.cancelled:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("hard duration left detached preparation executing")
	}
	if err := r.svc.checkEnvFree(bg, w.EnvID, ""); err == nil {
		t.Fatal("budget released environment while preparation cleanup was pending")
	}
	agg, err := r.store.LoadTask(bg, task)
	must(t, err)
	ended, _ := agg.Run(run)
	if ended.TerminalReason != "budget_breach" || agg.Task().State != domain.TaskFailed {
		t.Fatalf("terminal preparation=%+v", ended)
	}
	release()
	pollUntil(t, func() bool { return r.svc.startDoneChan(run) == nil && !r.svc.attached(run) })
	if err := r.svc.checkEnvFree(bg, w.EnvID, ""); err != nil {
		t.Fatalf("cleanup did not release ownership: %v", err)
	}
	if r.agent.Started() != 0 {
		t.Fatal("expired preparation launched an agent")
	}
	agg, err = r.store.LoadTask(bg, task)
	must(t, err)
	ended, _ = agg.Run(run)
	if ended.TerminalReason != "budget_breach" {
		t.Fatal("preparation cleanup replaced initiating budget reason")
	}
}

func TestCompletedRunsReleaseDurationAnchors(t *testing.T) {
	for _, ending := range []string{"completion", "cancel", "budget"} {
		t.Run(ending, func(t *testing.T) {
			r := newRig(t)
			r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: 10 * time.Second}}
			r.clock.now = t0.Add(8 * time.Second)
			r.svc.checkBudgets(bg, "t1", "r1")
			r.svc.mu.Lock()
			n := len(r.svc.durationAnchors)
			r.svc.mu.Unlock()
			if n != 1 {
				t.Fatalf("live duration anchors=%d", n)
			}
			switch ending {
			case "completion":
				must(t, r.svc.update(bg, "t1", func(a *domain.TaskAggregate) error { return a.StopRun("r1") }))
			case "cancel":
				must(t, r.svc.Cancel(bg, "t1"))
			case "budget":
				r.clock.now = t0.Add(10 * time.Second)
				r.svc.checkBudgets(bg, "t1", "r1")
			}
			r.svc.mu.Lock()
			n = len(r.svc.durationAnchors)
			r.svc.mu.Unlock()
			if n != 0 {
				t.Fatalf("ended run retained %d duration anchors", n)
			}
			ended, _ := r.load().Run("r1")
			if ended.DurationMillis < 8000 {
				t.Fatal("anchor cleanup erased persisted elapsed lifetime")
			}
		})
	}
}

func TestDurationSweepReclaimsAnchorRecreatedAfterTerminalCommit(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: 10 * time.Second}}
	r.clock.now = t0.Add(8 * time.Second)
	r.svc.checkBudgets(bg, "t1", "r1")
	r.svc.mu.Lock()
	anchor := r.svc.durationAnchors["r1"]
	r.svc.mu.Unlock()
	must(t, r.svc.Cancel(bg, "t1"))
	// An accounting attempt can lose its save race after computing its floor.
	r.svc.mu.Lock()
	r.svc.durationAnchors["r1"] = anchor
	r.svc.mu.Unlock()
	if errs := r.svc.scheduleDurationChecks(bg); len(errs) != 0 {
		t.Fatal(errs)
	}
	r.svc.mu.Lock()
	remaining := len(r.svc.durationAnchors)
	r.svc.mu.Unlock()
	if remaining != 0 {
		t.Fatal("terminal task excluded from active tasks retained a stale anchor")
	}
}

func TestDurationExpiryDropsOnlyOriginatingEgressWait(t *testing.T) {
	r := newWsRig(t)
	r.withEgressRequests()
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxDuration: time.Second}}
	w, a := r.create("duration-egress")
	task, run, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#7"})
	must(t, err)
	open := r.openEgress(task)
	r.clock.now = t0.Add(time.Second)
	r.svc.checkBudgets(bg, task, run)
	if len(r.svc.egressWaits) != 0 || r.svc.attached(run) {
		t.Fatal("expired run retained its egress wait or ownership")
	}
	must(t, r.svc.checkEnvFree(bg, w.EnvID, ""))
	nextTask, nextRun, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#8"})
	must(t, err)
	if err := r.svc.AnswerDecision(userContext(), open[0].ID, domain.Response{Option: domain.AnswerAllow, By: "werner", At: t0}); err == nil {
		t.Fatal("late answer for expired run was accepted")
	}
	r.svc.checkBudgets(bg, task, run)
	if !r.svc.attached(nextRun) || len(r.svc.egressWaits) != 1 {
		t.Fatal("stale budget check disturbed successor egress wait")
	}
	next, err := r.store.LoadTask(bg, nextTask)
	must(t, err)
	live, _ := next.Run(nextRun)
	if live.State.Terminal() || r.agent.Started() != 0 {
		t.Fatal("late answer or stale budget changed successor execution")
	}
}
