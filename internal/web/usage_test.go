package web

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/api"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

func urow(key, auth string, turns, in, out, cr, cw, reported, estimated, api, wall int64, share float64, label string) service.UsageRow {
	return service.UsageRow{
		UsageRow: store.UsageRow{
			Key: key, Auth: auth, Turns: turns, Runs: 1, Tokens: domain.UsageTokens{Input: in, Output: out, CacheRead: cr, CacheWrite: cw},
			ReportedMicroUSD: reported, EstimatedMicroUSD: estimated, APIMillis: api, WallMillis: wall, TurnsWithoutCost: 0,
		},
		Notional: auth == "subscription", CacheShare: &share, CostLabel: label,
	}
}

func usageFake(period string) service.UsageSummary {
	return service.UsageSummary{
		Period: period, Until: t0, Subscription: true, Code: store.CodeChanges{Approvals: 2, Files: 9, Added: 150, Removed: 57},
		Windows: []store.WindowReading{{Account: "claude", Name: "five_hour", Utilization: 0.42, ResetsAt: t0.Add(3 * time.Hour), At: t0}},
		Total:   []service.UsageRow{urow("all", "subscription", 5, 1000, 500, 4000, 100, 9000, 0, 90_000, 125_000, 0.8, "reported")},
		ByAgent: []service.UsageRow{
			urow("docs/review", "subscription", 2, 100, 50, 100, 0, 1000, 0, 1000, 2000, 0.5, "reported"),
			urow("docs/code", "subscription", 3, 900, 450, 3900, 100, 8000, 0, 89_000, 123_000, 0.8, "reported"),
		},
		ByModel: []service.UsageRow{urow("claude-opus-4-1", "api-key", 5, 1000, 500, 4000, 100, 9000, 700, 90_000, 125_000, 0.8, "mixed")},
	}
}

// The Harbor page leads its usage card with the usage window on a subscription, labels
// every cost, keeps an estimate apart from a reported figure, and sorts its rows.
func TestTheHarborShowsTheUsageCardForAPeriod(t *testing.T) {
	r := newRig(t)
	r.be.summary = usageFake
	b := r.browser()
	b.signIn()
	_, page := b.do("GET", "/", nil)
	for _, want := range []string{
		"Usage", `href="/?period=today"`, `href="/?period=30d"`, `aria-current="page"`, // the period links, 7 days by default
		`<meter id="win-five-hour" min="0" max="100" low="70" high="90" optimum="0" value="42">`, "resets in 3h 0m", "five hour", // the window leads as a meter
		"API-equivalent, not billed",   // and the cost is API-equivalent
		"All agents", "1m30s", "2m05s", // API time and wall time of the total
		"docs/review", "docs/code", "claude-opus-4-1",
		"$0.0090 + $0.0007", "mixed, spend", // an estimate is shown apart, and a key's cost is spend
		"80%", "50%", // the cache share
		"Code changes: 2 approved commit set(s): 9 file(s), +150 −57 lines", // the size of what was approved
		`href="/?agent=docs%2Freview&amp;period=7d"`,                        // a row links to that agent's tasks
		`href="/?period=7d&amp;sort=cost"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the usage card lacks %q", want)
		}
	}
	// the window comes before the table, as it leads on a subscription
	if strings.Index(page, "win-five-hour") > strings.Index(page, "All agents") {
		t.Error("the usage window does not lead the card")
	}
	if strings.Contains(page, "$0.0007 reported") || strings.Contains(page, "estimated mixed") {
		t.Error("an estimate is shown as reported")
	}
	// sorted by cost the dearer agent is first; by name the other way round
	_, byCost := b.do("GET", "/?sort=cost", nil)
	if strings.Index(byCost, "docs/code") > strings.Index(byCost, "docs/review") {
		t.Error("sorted by cost, the dearer agent is not first")
	}
	_, byTurns := b.do("GET", "/?sort=turns&period=30d", nil)
	if strings.Index(byTurns, "docs/code") > strings.Index(byTurns, "docs/review") {
		t.Error("sorted by turns, the busier agent is not first")
	}
	// an unknown period or sort falls back to the defaults and never breaks the page
	if resp, _ := b.do("GET", "/?period=forever&sort=%3B%20drop", nil); resp.StatusCode != 200 {
		t.Errorf("a bad period: %d", resp.StatusCode)
	}
}

// A row links to that agent's tasks, and the page shows only those.
func TestTheUsageCardLinksToAnAgentsTasks(t *testing.T) {
	r := newRig(t)
	r.be.summary = usageFake
	r.be.tasks = []store.TaskSummary{
		{ID: "t1", Repo: "o/a", Issue: "#1", State: domain.TaskRunning, Agent: "docs/review"},
		{ID: "t2", Repo: "o/b", Issue: "#2", State: domain.TaskRunning, Agent: "docs/code"},
	}
	b := r.browser()
	b.signIn()
	_, page := b.do("GET", "/?agent=docs/review", nil)
	if !strings.Contains(page, `href="/tasks/t1"`) || strings.Contains(page, `href="/tasks/t2"`) || !strings.Contains(page, "Tasks of docs/review") {
		t.Errorf("the agent's tasks are not filtered:\n%s", page)
	}
}

// noSummary hides the summary of a backend, as one that has none would.
type noSummary struct{ api.Backend }

// Without a summary (a backend that has none) the page has no card and still works.
func TestTheHarborHasNoUsageCardWithoutASummary(t *testing.T) {
	r := newRig(t)
	st, err := store.Open(bg, filepath.Join(t.TempDir(), "workharbor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ui, err := New(noSummary{r.be}, Options{Auth: r.auth, Store: st, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(ui.Handler())
	t.Cleanup(srv.Close)
	r.srv = srv
	b := r.browser()
	b.signIn()
	resp, page := b.do("GET", "/", nil)
	if resp.StatusCode != 200 || strings.Contains(page, `id="usage-h"`) {
		t.Errorf("%d, card shown: %v", resp.StatusCode, strings.Contains(page, `id="usage-h"`))
	}
}

// In api-key mode the card shows spend, and a quiet period says nothing was used.
func TestTheCardOnAnAPIKeyAndInAnEmptyPeriod(t *testing.T) {
	r := newRig(t)
	r.be.summary = func(period string) service.UsageSummary {
		if period == service.PeriodToday {
			return service.UsageSummary{Period: period}
		}
		return service.UsageSummary{Period: period, Total: []service.UsageRow{urow("all", "api-key", 1, 10, 5, 0, 0, 120, 0, 0, 0, 0, "reported")}}
	}
	b := r.browser()
	b.signIn()
	_, page := b.do("GET", "/?period=today", nil)
	if !strings.Contains(page, "Tokens and cost unknown: no agent reported usage in this period") {
		t.Errorf("an empty period:\n%s", page)
	}
	_, page = b.do("GET", "/?period=all", nil)
	if !strings.Contains(page, "reported, spend") || strings.Contains(page, "API-equivalent") || strings.Contains(page, "usage window leads") {
		t.Errorf("an API-key card:\n%s", page)
	}
}

// kept from #48's panel: the task page's own usage line.
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

func TestAUsageThatCannotBeReadLeavesTheCardOutAndIsReported(t *testing.T) {
	var reported []error
	r := newRigWith(t, func(o *Options) { o.OnError = func(err error) { reported = append(reported, err) } })
	r.be.failSummary = errors.New("database is locked")
	b := r.browser()
	b.signIn()
	resp, body := b.do("GET", "/", nil)
	if resp.StatusCode != 200 || strings.Contains(body, `id="usage-h"`) || !strings.Contains(body, "Needs you") {
		t.Errorf("status %d:\n%s", resp.StatusCode, body)
	}
	if len(reported) != 1 || !strings.Contains(reported[0].Error(), "database is locked") {
		t.Errorf("reported = %v", reported)
	}
}
