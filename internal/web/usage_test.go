package web

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

func subscriptionReport() service.UsageReport {
	return service.UsageReport{
		Windows: []store.WindowReading{
			{Account: "claude", Name: "five_hour", Utilization: 0.62, ResetsAt: t0.Add(2*time.Hour + 10*time.Minute), At: t0},
			{Account: "claude", Name: "seven_day", Utilization: 0.18, ResetsAt: t0.Add(30 * time.Hour), At: t0},
		},
		Rows: []service.UsageRow{{
			UsageRow: store.UsageRow{Auth: "subscription", Turns: 12, Tokens: domain.UsageTokens{Input: 1500, Output: 800, CacheRead: 20000}, ReportedMicroUSD: 1_230_000},
			Notional: true,
		}},
	}
}

func TestTheHarborLeadsWithTheAccountsUsageWindowsOnASubscription(t *testing.T) {
	r := newRig(t)
	r.be.usage = func(service.UsageQuery) (service.UsageReport, error) { return subscriptionReport(), nil }
	r.be.tasks = []store.TaskSummary{{ID: "t1", Repo: "wstein/workharbor", Issue: "#1", State: domain.TaskRunning}}
	b := r.browser()
	b.signIn()
	_, body := b.do("GET", "/", nil)
	for _, want := range []string{
		`<meter id="win-five-hour" min="0" max="100" low="70" high="90" optimum="0" value="62">`, ">62%<", "resets in 2h 10m",
		`id="win-seven-day"`, "resets in 1d 6h", "five hour", "Last 24 hours: 12 turns, 21.5k in, 800 out; $1.2300 reported, notional (subscription)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the harbor lacks %q:\n%s", want, body)
		}
	}
	// The windows come before the cost, and the usage before the tasks.
	win, cost, needs := strings.Index(body, "win-five-hour"), strings.Index(body, "Last 24 hours"), strings.Index(body, "Needs you")
	if win < 0 || win > cost || cost > needs {
		t.Errorf("the usage does not lead the page: window %d, cost %d, needs-you %d", win, cost, needs)
	}
	r.be.mu.Lock()
	defer r.be.mu.Unlock()
	if len(r.be.usageQueries) == 0 || r.be.usageQueries[0].Group != store.GroupAll || !r.be.usageQueries[0].Since.Equal(t0.Add(-24*time.Hour)) {
		t.Errorf("the usage was asked for as %+v", r.be.usageQueries)
	}
}

func TestInAPIKeyModeTheHarborShowsRealSpendAndTheBalance(t *testing.T) {
	r := newRig(t)
	r.be.usage = func(service.UsageQuery) (service.UsageReport, error) {
		return service.UsageReport{
			Rows:    []service.UsageRow{{UsageRow: store.UsageRow{Auth: "api-key", Turns: 3, Tokens: domain.UsageTokens{Input: 100, Output: 50}, ReportedMicroUSD: 420_000}}},
			Balance: &store.BalanceReading{Agent: "claude", RemainingMicroUSD: 7_500_000, At: t0},
		}, nil
	}
	b := r.browser()
	b.signIn()
	_, body := b.do("GET", "/", nil)
	for _, want := range []string{"Last 24 hours: 3 turns, 100 in, 50 out; $0.4200 reported", "Balance: $7.5000 left, as the agent reported"} {
		if !strings.Contains(body, want) {
			t.Errorf("the harbor lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "notional") || strings.Contains(body, "<meter") {
		t.Errorf("an api-key account shows subscription figures:\n%s", body)
	}
}

func TestTheHarborShowsNoUsagePanelWhenThereIsNoUsage(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	_, body := b.do("GET", "/", nil)
	if strings.Contains(body, `id="usage"`) || strings.Contains(body, "Last 24 hours") {
		t.Errorf("an empty usage panel:\n%s", body)
	}
}

func TestAUsageThatCannotBeReadLeavesThePanelOutAndIsReported(t *testing.T) {
	var reported []error
	r := newRigWith(t, func(o *Options) { o.OnError = func(err error) { reported = append(reported, err) } })
	r.be.usage = func(service.UsageQuery) (service.UsageReport, error) {
		return service.UsageReport{}, errors.New("database is locked")
	}
	b := r.browser()
	b.signIn()
	resp, body := b.do("GET", "/", nil)
	if resp.StatusCode != 200 || strings.Contains(body, `id="usage"`) || !strings.Contains(body, "Needs you") {
		t.Errorf("status %d:\n%s", resp.StatusCode, body)
	}
	if len(reported) != 1 || !strings.Contains(reported[0].Error(), "database is locked") {
		t.Errorf("reported = %v", reported)
	}
}

func TestATaskPageShowsItsUsageLine(t *testing.T) {
	r := newRig(t)
	r.be.show = func(id domain.ID) (service.TaskView, error) {
		return service.TaskView{Task: domain.Task{ID: id, Repo: "wstein/workharbor", Issue: "#7", State: domain.TaskRunning}}, nil
	}
	r.be.usage = func(q service.UsageQuery) (service.UsageReport, error) {
		if q.TaskID != "t7" || q.Group != store.GroupTask {
			t.Errorf("the usage was asked for as %+v", q)
		}
		rep := subscriptionReport()
		return rep, nil
	}
	b := r.browser()
	b.signIn()
	_, body := b.do("GET", "/tasks/t7", nil)
	want := `<p class="muted usage">usage: five-hour 62%; seven-day 18%; 12 turns, 21.5k in, 800 out; $1.2300 reported, notional (subscription)</p>`
	if !strings.Contains(body, want) {
		t.Errorf("the task page lacks its usage line:\n%s", body)
	}
}

func TestDurationsAreShortAndRoundedToTheMinute(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second: "1m", 12 * time.Minute: "12m", 2*time.Hour + 10*time.Minute: "2h 10m", 59*time.Minute + 40*time.Second: "1h 0m", 30 * time.Hour: "1d 6h",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
