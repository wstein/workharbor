package service

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/store"
)

func turn(at time.Time, model string, in, out, cr, cw, micro, api, wall int64) agent.Event {
	return agent.Event{Kind: agent.EventUsage, At: at, Usage: &agent.Usage{
		Model: model, Tokens: &agent.TokenCounts{Input: in, Output: out, CacheRead: cr, CacheWrite: cw},
		Cost: &agent.Cost{MicroUSD: micro, Source: agent.CostReported}, APIMillis: api, WallMillis: wall,
	}}
}

// The summary totals by agent and by model, each row with its cache share, its time,
// its runs and a cost label, and one row per auth mode in the total.
func TestTheSummaryTotalsByAgentAndModel(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	_, a1 := r.create("one")
	_, a2 := r.create("two")
	t1, run1, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a1.ID, Issue: "#1", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	t2, run2, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a2.ID, Issue: "#2", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	now := r.clock.now
	r.svc.recordUsage(bg, t1, run1, turn(now, "opus", 100, 50, 700, 200, 3000, 1000, 1500))
	r.svc.recordUsage(bg, t1, run1, turn(now.Add(time.Minute), "haiku", 10, 5, 0, 0, 100, 100, 150))
	r.svc.recordUsage(bg, t2, run2, turn(now.Add(2*time.Minute), "opus", 300, 20, 0, 0, 1000, 200, 300))

	sum, err := r.svc.UsageSummary(bg, PeriodAll, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Total) != 1 || sum.Total[0].Turns != 3 || sum.Total[0].Runs != 2 || sum.Total[0].ReportedMicroUSD != 4100 || sum.Total[0].APIMillis != 1300 || sum.Total[0].WallMillis != 1950 {
		t.Fatalf("total = %+v", sum.Total)
	}
	if !sum.Subscription || sum.Total[0].CostLabel != "reported" || !sum.Total[0].Notional {
		t.Errorf("subscription %v, label %q, notional %v: a subscription's cost is API-equivalent and labelled so", sum.Subscription, sum.Total[0].CostLabel, sum.Total[0].Notional)
	}
	by := map[string]UsageRow{}
	for _, row := range sum.ByAgent {
		by[row.Key] = row
	}
	if len(by) != 2 || by["one/docs"].Turns != 2 || by["two/docs"].Turns != 1 || by["one/docs"].ReportedMicroUSD != 3100 {
		t.Errorf("by agent = %+v", sum.ByAgent)
	}
	models := map[string]UsageRow{}
	for _, row := range sum.ByModel {
		models[row.Key] = row
	}
	opus := models["opus"]
	if len(models) != 2 || opus.Turns != 2 || opus.Tokens.Input != 400 || opus.Tokens.CacheRead != 700 || opus.Tokens.CacheWrite != 200 || opus.Tokens.Output != 70 {
		t.Fatalf("by model = %+v", sum.ByModel)
	}
	// 700 read of 400 + 700 + 200 input tokens
	if opus.CacheShare == nil || *opus.CacheShare < 0.5384 || *opus.CacheShare > 0.5385 {
		t.Errorf("opus cache share = %v, want 700/1300", opus.CacheShare)
	}
	if models["haiku"].CacheShare == nil || *models["haiku"].CacheShare != 0 {
		t.Errorf("haiku cache share = %v, want 0", models["haiku"].CacheShare)
	}
}

// Periods begin at the supervisor's midnight, not UTC's, and a run that spans
// midnight is split between the two days by its turns.
func TestPeriodsAndDaysAreTheSupervisorsAndARunMayStraddleMidnight(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	zone := time.FixedZone("UTC+10", 10*3600)
	r.svc.cfg.Location = zone
	// local: Oct 1 23:50 and Oct 2 00:10, and an old turn on Sep 20
	before := time.Date(2026, 10, 1, 23, 50, 0, 0, zone)
	after := time.Date(2026, 10, 2, 0, 10, 0, 0, zone)
	old := time.Date(2026, 9, 20, 12, 0, 0, 0, zone)
	r.svc.recordUsage(bg, "t1", "r1", turn(old, "m", 1, 1, 0, 0, 10, 0, 0))
	r.svc.recordUsage(bg, "t1", "r1", turn(before, "m", 1, 1, 0, 0, 100, 0, 0))
	r.svc.recordUsage(bg, "t1", "r1", turn(after, "m", 1, 1, 0, 0, 1000, 0, 0))
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, zone)

	cost := func(period string) (int64, int64) {
		sum, err := r.svc.UsageSummary(bg, period, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(sum.Total) == 0 {
			return 0, 0
		}
		return sum.Total[0].ReportedMicroUSD, sum.Total[0].Turns
	}
	// UTC midnight is Oct 2 10:00 local: a UTC "today" would miss the 00:10 turn
	if c, n := cost(PeriodToday); c != 1000 || n != 1 {
		t.Errorf("today = %d over %d turns, want only the turn after the supervisor's midnight", c, n)
	}
	if c, n := cost(Period7Days); c != 1100 || n != 2 {
		t.Errorf("7d = %d over %d turns", c, n)
	}
	if c, n := cost(Period30Days); c != 1110 || n != 3 {
		t.Errorf("30d = %d over %d turns", c, n)
	}
	if c, n := cost(PeriodAll); c != 1110 || n != 3 {
		t.Errorf("all = %d over %d turns", c, n)
	}
	rep, err := r.svc.Usage(bg, UsageQuery{Group: store.GroupDay})
	if err != nil {
		t.Fatal(err)
	}
	days := map[string]UsageRow{}
	for _, row := range rep.Rows {
		days[row.Key] = row
	}
	if len(days) != 3 || days["2026-10-01"].ReportedMicroUSD != 100 || days["2026-10-02"].ReportedMicroUSD != 1000 || days["2026-09-20"].ReportedMicroUSD != 10 {
		t.Errorf("by day = %+v: the days are the supervisor's", rep.Rows)
	}
	if days["2026-10-01"].Runs != 1 || days["2026-10-02"].Runs != 1 {
		t.Errorf("the run that spans midnight counts on both days: %+v", days)
	}
	if _, err := r.svc.UsageSummary(bg, "fortnight", now); err == nil {
		t.Error("an unknown period was accepted")
	}
}

// An estimate is kept apart from a reported cost and labelled, whatever else a
// group holds; a subscription row and an api-key row are never added together.
func TestAnEstimateIsNeverShownAsReportedAndAuthModesStayApart(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	at := t0.Add(time.Minute)
	appendRaw := func(task, runID domain.ID, auth, source string, micro int64) {
		ev, err := domain.NewUsageEvent(task, domain.UsageRecorded{
			RunID: runID, Repo: "wstein/workharbor", Agent: "claude-code", Auth: auth, Model: "m",
			Tokens: &domain.UsageTokens{Input: 10}, Cost: &domain.UsageCost{MicroUSD: micro, Source: domain.CostReported},
		}, at)
		if err != nil {
			t.Fatal(err)
		}
		if source != "reported" { // what #48's estimates will look like in the log
			ev.Payload = bytes.ReplaceAll(ev.Payload, []byte(`"reported"`), []byte(`"`+source+`"`))
		}
		if _, err := r.store.AppendUsage(bg, "claude-code", ev); err != nil {
			t.Fatal(err)
		}
	}
	appendRaw("t1", "r1", "api-key", "reported", 500)
	appendRaw("t1", "r1", "api-key", "estimated", 70)
	appendRaw("t1", "r2", "subscription", "reported", 9000)

	sum, err := r.svc.UsageSummary(bg, PeriodAll, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]UsageRow{}
	for _, row := range sum.Total {
		rows[row.Auth] = row
	}
	if len(rows) != 2 {
		t.Fatalf("total = %+v, want one row per auth mode", sum.Total)
	}
	key := rows["api-key"]
	if key.ReportedMicroUSD != 500 || key.EstimatedMicroUSD != 70 || key.CostLabel != "mixed" || key.Notional {
		t.Errorf("api-key row = %+v: 500 reported and 70 estimated, labelled mixed, real spend", key)
	}
	sub := rows["subscription"]
	if sub.ReportedMicroUSD != 9000 || sub.EstimatedMicroUSD != 0 || !sub.Notional {
		t.Errorf("subscription row = %+v", sub)
	}
	// a group of only estimates says estimated and reports nothing
	only, err := r.svc.Usage(bg, UsageQuery{Group: store.GroupModel})
	if err != nil || len(only.Rows) == 0 {
		t.Fatal(err)
	}
	for _, row := range only.Rows {
		if row.Auth == "api-key" && row.CostLabel != "mixed" {
			t.Errorf("label = %q", row.CostLabel)
		}
	}
}

// The summary carries the size of the commits approved in the period, from the
// approval entries only: a denial or an unapproved revision adds nothing.
func TestTheSummaryCarriesTheCodeOfApprovedCommits(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	zone := time.FixedZone("UTC+10", 10*3600)
	r.svc.cfg.Location = zone
	approve := func(at time.Time, files, added, removed int64) {
		ev := domain.Event{
			TaskID: "t1", Kind: domain.EventReviewApproved, Tier: domain.TierAudit, At: at,
			Payload: []byte(fmt.Sprintf(`{"decision":"d","sha":"s","by":"x","files":%d,"added":%d,"removed":%d}`, files, added, removed)),
		}
		if _, err := r.store.Append(bg, ev); err != nil {
			t.Fatal(err)
		}
	}
	approve(time.Date(2026, 10, 1, 23, 50, 0, 0, zone), 3, 40, 7) // yesterday, the supervisor's day
	approve(time.Date(2026, 10, 2, 0, 10, 0, 0, zone), 1, 10, 2)  // today
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, zone)
	today, err := r.svc.UsageSummary(bg, PeriodToday, now)
	if err != nil || today.Code != (store.CodeChanges{Approvals: 1, Files: 1, Added: 10, Removed: 2}) {
		t.Fatalf("today = %+v, %v", today.Code, err)
	}
	week, _ := r.svc.UsageSummary(bg, Period7Days, now)
	if week.Code != (store.CodeChanges{Approvals: 2, Files: 4, Added: 50, Removed: 9}) {
		t.Errorf("7d = %+v", week.Code)
	}
}
