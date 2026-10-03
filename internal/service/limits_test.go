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
