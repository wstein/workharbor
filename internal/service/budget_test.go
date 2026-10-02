package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/notify"
)

type recNotifier struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (n *recNotifier) Notify(_ context.Context, m notify.Message) error {
	n.mu.Lock()
	n.msgs = append(n.msgs, m)
	n.mu.Unlock()
	return nil
}

func (n *recNotifier) kinds() []notify.Kind {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []notify.Kind
	for _, m := range n.msgs {
		out = append(out, m.Kind)
	}
	return out
}

// turn is one reported turn of tokens and an optional cost.
func (r *rig) turn(i int, tokens int64, cost int64) {
	r.t.Helper()
	u := &agent.Usage{Model: "m"}
	if tokens > 0 {
		u.Tokens = &agent.TokenCounts{Input: tokens}
	}
	if cost > 0 {
		u.Cost = &agent.Cost{MicroUSD: cost, Source: agent.CostReported}
	}
	r.svc.recordUsage(bg, "t1", "r1", agent.Event{Kind: agent.EventUsage, At: t0.Add(time.Duration(i) * time.Minute), Usage: u})
}

func (r *rig) auditKinds(kind domain.EventKind) []domain.BudgetBreach {
	r.t.Helper()
	evs, err := r.store.EventsSince(bg, "t1", 0, 500)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []domain.BudgetBreach
	for _, e := range evs {
		if e.Kind == kind {
			var b domain.BudgetBreach
			if err := json.Unmarshal(e.Payload, &b); err != nil || e.Tier != domain.TierAudit {
				r.t.Fatalf("%s: %v tier %s", kind, err, e.Tier)
			}
			out = append(out, b)
		}
	}
	return out
}

func TestASoftThresholdWarnsOnceAndAHardLimitFailsTheTask(t *testing.T) {
	r := newRig(t)
	n := &recNotifier{}
	r.svc.cfg.Notifier = n
	r.svc.cfg.Budgets = Budgets{PerTask: Limit{MaxTokens: 1000}, SoftPercent: 80}

	r.turn(1, 500, 0) // 50%
	if len(r.auditKinds(domain.EventBudgetWarned)) != 0 {
		t.Fatal("warned below the threshold")
	}
	r.turn(2, 350, 0) // 85%: soft
	r.turn(3, 50, 0)  // 90%: still the same warning
	warned := r.auditKinds(domain.EventBudgetWarned)
	want := domain.BudgetBreach{Scope: domain.BudgetTask, Metric: domain.BudgetTokens, Limit: 1000, Used: 850}
	if len(warned) != 1 || warned[0] != want {
		t.Fatalf("warnings = %+v, want one %+v", warned, want)
	}
	if got := n.kinds(); len(got) != 1 || got[0] != notify.KindBudgetWarning {
		t.Errorf("notifications = %v", got)
	}
	if r.load().Task().State != domain.TaskRunning {
		t.Errorf("the task is %s below the hard limit", r.load().Task().State)
	}

	r.turn(4, 200, 0) // 1100: hard
	if st := r.load().Task().State; st != domain.TaskFailed {
		t.Fatalf("task = %s, want failed", st)
	}
	if r.runState() != domain.RunStopped {
		t.Errorf("run = %s, want stopped", r.runState())
	}
	ex := r.auditKinds(domain.EventBudgetExceeded)
	if len(ex) != 1 || ex[0].Used != 1100 || ex[0].Limit != 1000 || ex[0].Scope != domain.BudgetTask {
		t.Errorf("exceeded = %+v", ex)
	}
	if got := n.kinds(); got[len(got)-1] != notify.KindBudgetExceeded {
		t.Errorf("notifications = %v, want the exceeded one last", got)
	}
	for _, k := range n.kinds() {
		if k == notify.KindRunEnded {
			t.Error("a budget stop must say budget, not that the run ended")
		}
	}
	if len(r.errs) != 0 {
		t.Errorf("errors: %v", r.errs)
	}
	// Later reports of the stopped task change nothing and do not fail again.
	r.turn(5, 500, 0)
	if len(r.auditKinds(domain.EventBudgetExceeded)) != 1 || len(r.errs) != 0 {
		t.Errorf("a second exceed: %+v %v", r.auditKinds(domain.EventBudgetExceeded), r.errs)
	}
}

func TestACostBudgetCountsOnlyWhatTheAgentReported(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxCostMicroUSD: 1_000_000}}
	r.turn(1, 900_000, 0) // tokens and no cost: unknown cost, not a spend
	r.turn(2, 0, 400_000)
	if r.load().Task().State != domain.TaskRunning || len(r.auditKinds(domain.EventBudgetWarned)) != 0 {
		t.Fatal("unknown cost was counted")
	}
	r.turn(3, 0, 650_000) // 1.05 USD reported
	ex := r.auditKinds(domain.EventBudgetExceeded)
	if r.load().Task().State != domain.TaskFailed || len(ex) != 1 || ex[0].Scope != domain.BudgetRun || ex[0].Metric != domain.BudgetCost || ex[0].RunID != "r1" || ex[0].Used != 1_050_000 {
		t.Errorf("task = %s, exceeded = %+v", r.load().Task().State, ex)
	}
}

func TestNoBudgetsMeansNoLimit(t *testing.T) {
	r := newRig(t)
	r.turn(1, 1_000_000_000, 5_000_000_000)
	if r.load().Task().State != domain.TaskRunning || len(r.auditKinds(domain.EventBudgetExceeded))+len(r.auditKinds(domain.EventBudgetWarned)) != 0 {
		t.Error("a budget was applied that nobody set")
	}
}

func TestARunBudgetSeesOnlyItsRun(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerRun: Limit{MaxTokens: 1000}}
	// Another run of the same task used 900 tokens earlier.
	r.svc.recordUsage(bg, "t1", "r0", agent.Event{Kind: agent.EventUsage, At: t0, Usage: &agent.Usage{Model: "m", Tokens: &agent.TokenCounts{Input: 900}}})
	r.turn(1, 500, 0)
	if r.load().Task().State != domain.TaskRunning {
		t.Fatalf("the budget of run r1 counted run r0: task %s", r.load().Task().State)
	}
	r.turn(2, 600, 0)
	if r.load().Task().State != domain.TaskFailed {
		t.Errorf("task = %s, want failed at 1100 tokens of run r1", r.load().Task().State)
	}
}

func TestSoftPercentDefaultsAndIsBounded(t *testing.T) {
	for in, want := range map[int]int64{0: 80, 50: 50, 99: 99, 100: 80, -3: 80} {
		if got := (Budgets{SoftPercent: in}).soft(); got != want {
			t.Errorf("SoftPercent %d = %d, want %d", in, got, want)
		}
	}
}
