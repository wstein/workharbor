package service

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/notify"
)

func (r *rig) report(i int, u *agent.Usage) {
	r.t.Helper()
	r.svc.recordUsage(bg, "t1", "r1", agent.Event{Kind: agent.EventUsage, At: t0.Add(time.Duration(i) * time.Minute), Usage: u})
}

// Until an adapter reports, the provider is unknown: no windows, no balance.
func TestLimitsAreUnknownUntilAnAdapterReports(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	ps, err := r.svc.Limits(bg)
	if err != nil || len(ps) != 1 {
		t.Fatalf("limits = %+v, %v", ps, err)
	}
	if p := ps[0]; p.Known || p.Low || len(p.Windows) != 0 || p.Balance != nil || !strings.Contains(p.Source, "not reported") {
		t.Errorf("provider = %+v, want unknown", p)
	}
	r.report(1, &agent.Usage{Model: "m", Tokens: &agent.TokenCounts{Input: 10}}) // tokens alone say nothing of a limit
	if ps, _ = r.svc.Limits(bg); ps[0].Known {
		t.Error("a report without a window or balance made the limit known")
	}
}

// The figures are the adapter's own, and a low one warns once per reading period.
func TestALowReportedLimitIsFlaggedAndNotifiedOnce(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	n := &recNotifier{}
	r.svc.cfg.Notifier = n
	r.svc.cfg.LowLimits = LowLimits{WindowPercent: 80, BalanceMicroUSD: 1_000_000}
	reset := t0.Add(4 * time.Hour)
	win := func(u float64) *agent.Usage {
		return &agent.Usage{Model: "m", Windows: []agent.UsageWindow{{Name: agent.WindowFiveHour, Utilization: u, ResetsAt: reset}}}
	}
	r.report(1, win(0.5))
	if len(n.kinds()) != 0 {
		t.Fatal("notified below the threshold")
	}
	r.report(2, win(0.85))
	r.report(3, win(0.9)) // the same window, still low: no second push
	if got := n.kinds(); len(got) != 1 || got[0] != notify.KindLimitLow {
		t.Fatalf("notifications = %v, want one limit_low", got)
	}
	ps, _ := r.svc.Limits(bg)
	p := ps[0]
	if !p.Known || !p.Low || len(p.Windows) != 1 || p.Windows[0].Used != 0.9 || !p.Windows[0].ResetsAt.Equal(reset) {
		t.Errorf("provider = %+v", p)
	}
	r.report(4, &agent.Usage{Model: "m", Balance: &agent.Balance{RemainingMicroUSD: 500_000}})
	if got := n.kinds(); len(got) != 2 {
		t.Errorf("notifications = %v, want the balance to warn", got)
	}
	if ps, _ = r.svc.Limits(bg); !ps[0].LowBalance {
		t.Error("the low balance is not flagged")
	}
}

// An API-key provider carries its configured budget (D41); a subscription does not.
func TestAnAPIKeyProviderShowsItsConfiguredBudget(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.svc.cfg.Budgets = Budgets{PerTask: Limit{MaxCostMicroUSD: 5_000_000}}
	if ps, _ := r.svc.Limits(bg); ps[0].BudgetMicroUSD != 0 || ps[0].APIKey {
		t.Errorf("provider = %+v, want no budget before an API-key turn", ps[0])
	}
	r.svc.cfg.Spec = func(domain.Task, domain.Run) agent.StartSpec { return agent.StartSpec{Auth: agent.AuthAPIKey} }
	r.report(1, &agent.Usage{Model: "m", Tokens: &agent.TokenCounts{Input: 1}})
	if ps, _ := r.svc.Limits(bg); !ps[0].APIKey || ps[0].BudgetMicroUSD != 5_000_000 || ps[0].Known {
		t.Errorf("provider = %+v, want the budget and still unknown", ps[0])
	}
}

// D40: the limits come from the usage reports through the store and nothing else:
// the file imports no package that could read a credential, a login or a vendor API.
func TestLimitsReadNoCredential(t *testing.T) {
	t.Parallel()
	f, err := parser.ParseFile(token.NewFileSet(), "limits.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		switch path {
		case "os", "os/exec", "net", "net/http", "io/ioutil", "path/filepath", "io/fs":
			t.Errorf("limits.go imports %q: a limit reading must come from the adapters' reports only (D40)", path)
		}
	}
}

// The push key is the window name: a moving resets_at or a flood of names does
// not mint pushes without bound, and a recovered window may warn again.
func TestLowWindowPushesAreKeyedByNameAndBounded(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	n := &recNotifier{}
	r.svc.cfg.Notifier = n
	r.svc.cfg.LowLimits = LowLimits{WindowPercent: 80}
	win := func(name string, u float64, reset time.Time) *agent.Usage {
		return &agent.Usage{Model: "m", Windows: []agent.UsageWindow{{Name: name, Utilization: u, ResetsAt: reset}}}
	}
	later := r.clock.now.Add(100 * time.Hour)
	for i := 1; i <= 5; i++ { // resets_at moves every turn
		r.report(i, win("five_hour", 0.95, later.Add(time.Duration(i)*time.Second)))
	}
	if got := n.kinds(); len(got) != 1 {
		t.Fatalf("notifications = %v, want one", got)
	}
	r.report(6, win("five_hour", 0.1, later)) // recovered: the key is cleared
	r.clock.now = r.clock.now.Add(limitWarnCooldown + time.Minute)
	r.report(7, win("five_hour", 0.95, time.Time{}))
	if got := n.kinds(); len(got) != 2 {
		t.Errorf("notifications = %v, want a second after recovery", got)
	}
	for i := 0; i < 500; i++ {
		r.report(10+i, win("w"+strconv.Itoa(i), 0.99, later))
	}
	r.svc.mu.Lock()
	size := len(r.svc.limitWarned)
	r.svc.mu.Unlock()
	if size > maxLimitWarned {
		t.Errorf("limitWarned has %d keys, want at most %d", size, maxLimitWarned)
	}
	if got := n.kinds(); len(got) > 500+2 {
		t.Errorf("%d pushes for 500 window names", len(got))
	}
}

// A flood of made-up window names does not silence a real window or balance.
func TestAWindowFloodDoesNotSilenceARealWarning(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	n := &recNotifier{}
	r.svc.cfg.Notifier = n
	r.svc.cfg.LowLimits = LowLimits{WindowPercent: 80, BalanceMicroUSD: 1_000_000}
	later := r.clock.now.Add(100 * time.Hour)
	for i := 0; i < 200; i++ {
		r.report(i+1, &agent.Usage{Model: "m", Windows: []agent.UsageWindow{{Name: "w" + strconv.Itoa(i), Utilization: 0.99, ResetsAt: later}}})
	}
	before := len(n.kinds())
	r.report(1000, &agent.Usage{Model: "m", Windows: []agent.UsageWindow{{Name: agent.WindowFiveHour, Utilization: 0.95, ResetsAt: later}}})
	if got := len(n.kinds()); got != before+1 {
		t.Errorf("pushes %d -> %d, want the real five_hour window to warn", before, got)
	}
	r.report(1001, &agent.Usage{Model: "m", Balance: &agent.Balance{RemainingMicroUSD: 1}})
	if got := len(n.kinds()); got != before+2 {
		t.Errorf("pushes = %d, want the balance to warn", got)
	}
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	if len(r.svc.limitWarned) > maxLimitWarned+1 {
		t.Errorf("limitWarned has %d keys", len(r.svc.limitWarned))
	}
}

// A reading that alternates around the threshold pushes once per cooldown.
func TestAnAlternatingReadingWarnsOncePerCooldown(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	n := &recNotifier{}
	r.svc.cfg.Notifier = n
	r.svc.cfg.LowLimits = LowLimits{WindowPercent: 80}
	win := func(u float64) *agent.Usage {
		return &agent.Usage{Model: "m", Windows: []agent.UsageWindow{{Name: agent.WindowFiveHour, Utilization: u}}}
	}
	for i, u := range []float64{0.95, 0.10, 0.95, 0.10, 0.95} {
		r.report(i+1, win(u))
	}
	if got := len(n.kinds()); got != 1 {
		t.Fatalf("pushes = %d, want 1 inside the cooldown", got)
	}
	r.clock.now = r.clock.now.Add(limitWarnCooldown + time.Minute)
	r.report(10, win(0.10))
	r.report(11, win(0.95))
	if got := len(n.kinds()); got != 2 {
		t.Errorf("pushes = %d, want a second after the cooldown", got)
	}
}

// A reading whose own reset time has passed is shown but not called low.
func TestAWindowIsNotLowAfterItsReset(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.svc.cfg.LowLimits = LowLimits{WindowPercent: 80}
	r.report(1, &agent.Usage{Model: "m", Windows: []agent.UsageWindow{{Name: agent.WindowFiveHour, Utilization: 0.95, ResetsAt: r.clock.now.Add(time.Hour)}}})
	if ps, _ := r.svc.Limits(bg); !ps[0].Low || !ps[0].Windows[0].Low {
		t.Fatal("not low before the reset")
	}
	r.clock.now = r.clock.now.Add(2 * time.Hour)
	ps, _ := r.svc.Limits(bg)
	if ps[0].Low || ps[0].Windows[0].Low || ps[0].Windows[0].Used != 0.95 {
		t.Errorf("after the reset = %+v, want the reading kept and not low", ps[0])
	}
}
