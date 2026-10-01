package service

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/store"
	"github.com/wstein/workharbor/internal/usage"
)

var updateUsage = flag.Bool("update-usage", false, "rewrite the usage golden file")

// recordedUsage reads the usage events of the real Claude Code runs recorded in
// spike #7 (the adapter's golden files), as the adapter produced them.
func recordedUsage(t *testing.T) []agent.Event {
	t.Helper()
	files, _ := filepath.Glob("../agent/claude/testdata/recorded/*.golden.json")
	if len(files) == 0 {
		t.Fatal("no recorded fixtures")
	}
	var out []agent.Event
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // a fixture of this repository
		if err != nil {
			t.Fatal(err)
		}
		var g struct {
			Events []agent.Event `json:"events"`
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatal(err)
		}
		for _, e := range g.Events {
			if e.Kind == agent.EventUsage {
				out = append(out, e)
			}
		}
	}
	return out
}

func TestUsageFromRecordedRunsAddsUpAndSurvivesAPurge(t *testing.T) {
	r := newRig(t)
	events := recordedUsage(t)
	var wantTokens domain.UsageTokens
	var wantCost int64
	for _, e := range events {
		if e.Usage.Tokens == nil || e.Usage.Cost == nil {
			t.Fatalf("a recorded run lacks tokens or cost: %+v", e.Usage)
		}
		wantTokens.Input += e.Usage.Tokens.Input
		wantTokens.Output += e.Usage.Tokens.Output
		wantTokens.CacheRead += e.Usage.Tokens.CacheRead
		wantTokens.CacheWrite += e.Usage.Tokens.CacheWrite
		wantCost += e.Usage.Cost.MicroUSD
		r.svc.recordUsage(bg, "t1", "r1", e)
	}
	if len(r.errs) != 0 {
		t.Fatalf("errors: %v", r.errs)
	}
	check := func(when string) {
		rep, err := r.svc.Usage(bg, UsageQuery{TaskID: "t1", Group: store.GroupTask})
		if err != nil || len(rep.Rows) != 1 {
			t.Fatalf("%s: %+v, %v", when, rep, err)
		}
		row := rep.Rows[0]
		if row.Turns != int64(len(events)) || row.Tokens != wantTokens || row.ReportedMicroUSD != wantCost || row.EstimatedMicroUSD != 0 {
			t.Errorf("%s: row = %+v, want %d turns, %+v tokens, %d reported", when, row, len(events), wantTokens, wantCost)
		}
		if row.Auth != "subscription" || !row.Notional || row.Key != "t1" {
			t.Errorf("%s: auth %q notional %v key %q", when, row.Auth, row.Notional, row.Key)
		}
	}
	check("after recording")
	if _, err := r.store.Purge(bg, store.PurgeSpec{TaskID: "t1", Actor: "werner", All: true}); err != nil {
		t.Fatal(err)
	}
	check("after a purge")
	line, err := r.svc.UsageLine(bg, "t1")
	if err != nil || !strings.Contains(line, "reported") || !strings.Contains(line, "notional (subscription)") || strings.Contains(line, "estimated") {
		t.Errorf("usage line = %q, %v", line, err)
	}
}

func TestACostTheAgentDidNotReportIsEstimatedAndLabelled(t *testing.T) {
	r := newRig(t)
	at := t0.Add(time.Minute)
	turn := agent.Event{Kind: agent.EventUsage, At: at, Usage: &agent.Usage{
		Model: "m", Tokens: &agent.TokenCounts{Input: 1_000_000, Output: 1_000_000},
	}}
	// No price table: tokens are recorded, no cost is invented.
	r.svc.recordUsage(bg, "t1", "r1", turn)
	rows, _ := r.store.UsageTotals(bg, store.UsageFilter{}, store.GroupAll)
	if len(rows) != 1 || rows[0].ReportedMicroUSD != 0 || rows[0].EstimatedMicroUSD != 0 || rows[0].TurnsWithoutCost != 1 || rows[0].Tokens.Input != 1_000_000 {
		t.Fatalf("without prices: %+v", rows)
	}
	r.svc.cfg.Prices = usage.PriceTable{Version: "2026-10-01", Models: map[string]usage.Price{"m": {Input: 3_000_000, Output: 15_000_000}}}
	r.svc.recordUsage(bg, "t1", "r1", turn)
	// A cost the agent reports is never replaced by an estimate.
	reported := turn
	reported.Usage = &agent.Usage{Model: "m", Tokens: turn.Usage.Tokens, Cost: &agent.Cost{MicroUSD: 7, Source: agent.CostReported}}
	r.svc.recordUsage(bg, "t1", "r1", reported)
	rows, _ = r.store.UsageTotals(bg, store.UsageFilter{}, store.GroupAll)
	if rows[0].EstimatedMicroUSD != 18_000_000 || rows[0].ReportedMicroUSD != 7 || rows[0].TurnsWithoutCost != 1 {
		t.Errorf("rows = %+v", rows[0])
	}
	evs, _ := r.store.EventsSince(bg, "t1", 0, 50)
	var est *domain.UsageCost
	for _, e := range evs {
		var u domain.UsageRecorded
		if e.Kind == domain.EventUsage && json.Unmarshal(e.Payload, &u) == nil && u.Cost != nil && u.Cost.Source == domain.CostEstimated {
			est = u.Cost
			if e.Tier != domain.TierAudit {
				t.Errorf("a usage entry is audit tier, got %s", e.Tier)
			}
		}
	}
	if est == nil || est.PriceTable != "2026-10-01" {
		t.Errorf("an estimate must name its price table: %+v", est)
	}
	line := FormatUsageLine(UsageReport{Rows: []UsageRow{{UsageRow: rows[0]}}})
	if !strings.Contains(line, "$0.0000 reported") && !strings.Contains(line, "estimated") {
		t.Errorf("line = %q", line)
	}
}

func TestSubscriptionWindowIsOneFigureAcrossTasks(t *testing.T) {
	r := newRig(t)
	win := func(u float64) []agent.UsageWindow {
		return []agent.UsageWindow{{Name: agent.WindowFiveHour, Utilization: u, ResetsAt: t0.Add(4 * time.Hour)}}
	}
	// A second task on the same account reports a later, higher reading.
	a := domain.NewTaskAggregate(domain.Task{ID: "t2", Repo: "wstein/other", Issue: "#1", State: domain.TaskRunning, CreatedAt: t0})
	a.AddEnvironment(domain.Environment{ID: r.env, Backend: "fake", State: domain.EnvRunning})
	must(t, a.StartRun(domain.Run{ID: "r2", WorkspaceID: "w1", EnvID: r.env}))
	must(t, a.MarkRunning("r2"))
	if _, err := r.store.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	r.svc.recordUsage(bg, "t1", "r1", agent.Event{Kind: agent.EventUsage, At: t0.Add(time.Minute), Usage: &agent.Usage{Model: "m", Windows: win(0.3)}})
	r.svc.recordUsage(bg, "t2", "r2", agent.Event{Kind: agent.EventUsage, At: t0.Add(2 * time.Minute), Usage: &agent.Usage{Model: "m", Windows: win(0.6)}})
	rep, err := r.svc.Usage(bg, UsageQuery{TaskID: "t1"})
	if err != nil || len(rep.Windows) != 1 || rep.Windows[0].Utilization != 0.6 {
		t.Fatalf("the report of task t1 must show the account's window: %+v, %v", rep.Windows, err)
	}
	line := FormatUsageLine(rep)
	if !strings.HasPrefix(line, "usage: five-hour 60%;") {
		t.Errorf("a subscription leads with its window: %q", line)
	}
}

func TestAMalformedUsageReportIsDropped(t *testing.T) {
	r := newRig(t)
	r.svc.recordUsage(bg, "t1", "r1", agent.Event{Kind: agent.EventUsage, Usage: &agent.Usage{Model: "m", Tokens: &agent.TokenCounts{Input: -1}}})
	r.svc.recordUsage(bg, "t1", "r1", agent.Event{Kind: agent.EventUsage})
	if len(r.errs) != 1 {
		t.Errorf("errors = %v, want one for the negative count", r.errs)
	}
	if rows, _ := r.store.UsageTotals(bg, store.UsageFilter{}, store.GroupAll); len(rows) != 0 {
		t.Errorf("a malformed report was recorded: %+v", rows)
	}
}

func TestFormatting(t *testing.T) {
	for micro, want := range map[int64]string{0: "$0.0000", 5804: "$0.0058", 999_999: "$1.0000", 1_234_567: "$1.2346", 12_000_000: "$12.0000"} {
		if got := FormatMicroUSD(micro); got != want {
			t.Errorf("FormatMicroUSD(%d) = %s, want %s", micro, got, want)
		}
	}
	for n, want := range map[int64]string{999: "999", 1500: "1.5k", 2_500_000: "2.5M"} {
		if got := Compact(n); got != want {
			t.Errorf("Compact(%d) = %s", n, got)
		}
	}
	if FormatUsageLine(UsageReport{}) != "" {
		t.Error("no usage, no line")
	}
}

// The JSON of a report is a contract of `whr usage --json` and the UI.
func TestUsageReportJSONIsStable(t *testing.T) {
	d := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	rep := UsageReport{
		Group: store.GroupTask,
		Since: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Rows: []UsageRow{
			{UsageRow: store.UsageRow{Key: "t1", Auth: "api-key", Turns: 2, Tokens: domain.UsageTokens{Input: 18, Output: 194, CacheRead: 43502, CacheWrite: 233}, ReportedMicroUSD: 5804, EstimatedMicroUSD: 120, TurnsWithoutCost: 1, First: d, Last: d.Add(time.Hour)}},
			{UsageRow: store.UsageRow{Key: "t1", Auth: "subscription", Turns: 1, TurnsWithoutToken: 1, ReportedMicroUSD: 1, First: d, Last: d}, Notional: true},
		},
		Windows: []store.WindowReading{{Account: "claude", Name: "five_hour", Utilization: 0.42, ResetsAt: d.Add(3 * time.Hour), At: d}},
	}
	got, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "usage_report.golden.json")
	if *updateUsage {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // a fixture of this repository
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("the report JSON changed:\n%s", got)
	}
	empty, _ := json.Marshal(UsageReport{Rows: []UsageRow{}, Windows: []store.WindowReading{}})
	if !strings.Contains(string(empty), `"rows":[]`) || !strings.Contains(string(empty), `"windows":[]`) {
		t.Errorf("an empty report has empty lists, not null: %s", empty)
	}
}
