package store

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

func usageEvent(t *testing.T, task, run, repo, auth string, tok *domain.UsageTokens, cost *domain.UsageCost, at time.Time, windows ...domain.UsageWindow) domain.Event {
	t.Helper()
	ev, err := domain.NewUsageEvent(domain.ID(task), domain.UsageRecorded{
		RunID: domain.ID(run), Repo: repo, Agent: "claude", Auth: auth, Model: "m", Tokens: tok, Cost: cost, Windows: windows,
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestUsageTotalsKeepAuthAndUnknownsApart(t *testing.T) {
	s := openTemp(t)
	day1 := time.Date(2026, 10, 1, 23, 0, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Hour)
	tok := func(i, o, r, w int64) *domain.UsageTokens {
		return &domain.UsageTokens{Input: i, Output: o, CacheRead: r, CacheWrite: w}
	}
	rep := func(n int64) *domain.UsageCost { return &domain.UsageCost{MicroUSD: n, Source: domain.CostReported} }
	for _, e := range []domain.Event{
		usageEvent(t, "t1", "r1", "a/b", "subscription", tok(10, 20, 30, 40), rep(100), day1),
		usageEvent(t, "t1", "r1", "a/b", "subscription", tok(1, 2, 3, 4), rep(50), day2),
		usageEvent(t, "t1", "r2", "a/b", "api-key", nil, nil, day2), // nothing reported
		usageEvent(t, "t2", "r3", "c/d", "api-key", tok(5, 5, 5, 5), rep(7), day2),
	} {
		if _, err := s.AppendUsage(bg, "claude", e); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.UsageTotals(bg, UsageFilter{TaskID: "t1"}, GroupTask)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("one task with two auth modes is two rows: %+v", rows)
	}
	api, sub := rows[0], rows[1] // ordered by auth
	if api.Auth != "api-key" || api.Turns != 1 || api.TurnsWithoutToken != 1 || api.TurnsWithoutCost != 1 || api.Tokens != (domain.UsageTokens{}) {
		t.Errorf("api-key row = %+v: an unreported turn is counted, not read as zero spend", api)
	}
	want := domain.UsageTokens{Input: 11, Output: 22, CacheRead: 33, CacheWrite: 44}
	if sub.Auth != "subscription" || sub.Turns != 2 || sub.Tokens != want || sub.ReportedMicroUSD != 150 || sub.TurnsWithoutCost != 0 {
		t.Errorf("subscription row = %+v: reported and estimated are separate columns", sub)
	}
	if !sub.First.Equal(day1) || !sub.Last.Equal(day2) {
		t.Errorf("first/last = %v / %v", sub.First, sub.Last)
	}

	for group, keys := range map[UsageGroup][]string{
		GroupRun: {"r1", "r2", "r3"}, GroupRepo: {"a/b", "c/d"}, GroupDay: {"2026-10-01", "2026-10-02"}, GroupMonth: {"2026-10"}, GroupAll: {"all"},
	} {
		rows, err := s.UsageTotals(bg, UsageFilter{}, group)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range rows {
			if len(got) == 0 || got[len(got)-1] != r.Key {
				got = append(got, r.Key)
			}
		}
		if !reflect.DeepEqual(got, keys) {
			t.Errorf("group %s: keys %v, want %v", group, got, keys)
		}
	}

	// The period filter: Since is inclusive, Until exclusive.
	rows, _ = s.UsageTotals(bg, UsageFilter{Since: day2}, GroupAll)
	var turns int64
	for _, r := range rows {
		turns += r.Turns
	}
	if turns != 3 {
		t.Errorf("turns since day2 = %d, want 3", turns)
	}
	if rows, _ = s.UsageTotals(bg, UsageFilter{Until: day1}, GroupAll); len(rows) != 0 {
		t.Errorf("until day1 (exclusive) = %+v", rows)
	}
	if rows, _ = s.UsageTotals(bg, UsageFilter{Repo: "c/d"}, GroupAll); len(rows) != 1 || rows[0].ReportedMicroUSD != 7 {
		t.Errorf("repo filter = %+v", rows)
	}
	if _, err := s.UsageTotals(bg, UsageFilter{}, "nope"); err == nil {
		t.Error("an unknown grouping must fail")
	}
}

// Usage rows are audit entries: a purge of the transcript leaves the totals.
func TestUsageSurvivesAPurge(t *testing.T) {
	s := openTemp(t)
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if _, err := s.AppendUsage(bg, "claude", usageEvent(t, "t1", "r1", "a/b", "api-key", &domain.UsageTokens{Input: 9}, &domain.UsageCost{MicroUSD: 5, Source: domain.CostReported}, at)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(bg, transcript("t1", "a message")); err != nil {
		t.Fatal(err)
	}
	before, _ := s.UsageTotals(bg, UsageFilter{}, GroupAll)
	if _, err := s.Purge(bg, PurgeSpec{TaskID: "t1", Actor: "werner", All: true}); err != nil {
		t.Fatal(err)
	}
	after, err := s.UsageTotals(bg, UsageFilter{}, GroupAll)
	if err != nil || !reflect.DeepEqual(before, after) || len(after) != 1 || after[0].Tokens.Input != 9 {
		t.Errorf("totals changed by a purge: %+v -> %+v (%v)", before, after, err)
	}
}

func TestUsageWindowsAreOneFigurePerAccount(t *testing.T) {
	s := openTemp(t)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	reset := t0.Add(3 * time.Hour)
	w := func(name string, u float64) domain.UsageWindow {
		return domain.UsageWindow{Name: name, Utilization: u, ResetsAt: reset}
	}
	// Two tasks on one account report; the later reading wins, whichever task.
	for _, e := range []domain.Event{
		usageEvent(t, "t1", "r1", "a/b", "subscription", nil, nil, t0, w("five_hour", 0.2), w("seven_day", 0.1)),
		usageEvent(t, "t2", "r2", "c/d", "subscription", nil, nil, t0.Add(time.Minute), w("five_hour", 0.5)),
	} {
		if _, err := s.AppendUsage(bg, "claude", e); err != nil {
			t.Fatal(err)
		}
	}
	// A late event, delivered after a newer one, does not roll a window back.
	if _, err := s.AppendUsage(bg, "claude", usageEvent(t, "t1", "r1", "a/b", "subscription", nil, nil, t0.Add(-time.Hour), w("five_hour", 0.05))); err != nil {
		t.Fatal(err)
	}
	got, err := s.UsageWindows(bg, "claude")
	if err != nil || len(got) != 2 {
		t.Fatalf("windows = %+v, %v", got, err)
	}
	if got[0].Name != "five_hour" || got[0].Utilization != 0.5 || !got[0].ResetsAt.Equal(reset) || got[1].Name != "seven_day" || got[1].Utilization != 0.1 {
		t.Errorf("windows = %+v", got)
	}
	if other, _ := s.UsageWindows(bg, "codex"); len(other) != 0 {
		t.Errorf("another account has no windows: %+v", other)
	}
}

func TestAppendUsageRefusesOtherEvents(t *testing.T) {
	s := openTemp(t)
	if _, err := s.AppendUsage(bg, "claude", audit("t1", domain.EventTaskState)); err == nil {
		t.Error("only a usage audit entry may be appended here")
	}
}

func TestLatestBalanceIsAReadingFromTheAudit(t *testing.T) {
	s := openTemp(t)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if b, err := s.LatestBalance(bg, "claude"); err != nil || b != nil {
		t.Fatalf("no balance reported yet: %+v %v", b, err)
	}
	with := func(task string, at time.Time, micro int64) domain.Event {
		ev, err := domain.NewUsageEvent(domain.ID(task), domain.UsageRecorded{
			RunID: "r1", Agent: "claude", Auth: "api-key", Model: "m", Balance: &domain.UsageBalance{RemainingMicroUSD: micro},
		}, at)
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	for _, e := range []domain.Event{
		with("t1", t0, 9_000_000), with("t2", t0.Add(time.Hour), 8_500_000),
		usageEvent(t, "t1", "r1", "a/b", "api-key", nil, nil, t0.Add(2*time.Hour)), // reports no balance: not a zero balance
	} {
		if _, err := s.AppendUsage(bg, "claude", e); err != nil {
			t.Fatal(err)
		}
	}
	b, err := s.LatestBalance(bg, "claude")
	if err != nil || b == nil || b.RemainingMicroUSD != 8_500_000 || !b.At.Equal(t0.Add(time.Hour)) {
		t.Errorf("latest balance = %+v, %v", b, err)
	}
	if _, err := s.Purge(bg, PurgeSpec{TaskID: "t2", Actor: "werner", All: true}); err != nil {
		t.Fatal(err)
	}
	if b2, _ := s.LatestBalance(bg, "claude"); b2 == nil || b2.RemainingMicroUSD != 8_500_000 {
		t.Errorf("a purge changed the balance: %+v", b2)
	}
	if other, _ := s.LatestBalance(bg, "codex"); other != nil {
		t.Errorf("another agent has no balance: %+v", other)
	}
}

// The code changes of a period are the sum of its review.approved entries, and the
// size a candidate was pinned with survives a save and a load.
func TestCodeChangesSumTheApprovalsOfAPeriod(t *testing.T) {
	s := openTemp(t)
	at := time.Unix(1_700_000_000, 0)
	approve := func(task domain.ID, when time.Time, files, added, removed int64) {
		ev := domain.Event{
			TaskID: task, Kind: domain.EventReviewApproved, Tier: domain.TierAudit, At: when,
			Payload: []byte(fmt.Sprintf(`{"decision":"d","sha":"s","by":"x","files":%d,"added":%d,"removed":%d}`, files, added, removed)),
		}
		if _, err := s.Append(bg, ev); err != nil {
			t.Fatal(err)
		}
	}
	approve("t1", at, 3, 40, 7)
	approve("t2", at.Add(2*time.Hour), 1, 10, 0)
	approve("t3", at.Add(48*time.Hour), 5, 100, 50)
	all, err := s.CodeChanges(bg, time.Time{}, time.Time{})
	if err != nil || all != (CodeChanges{Approvals: 3, Files: 9, Added: 150, Removed: 57}) {
		t.Fatalf("all = %+v, %v", all, err)
	}
	day, _ := s.CodeChanges(bg, at.Add(-time.Hour), at.Add(24*time.Hour))
	if day != (CodeChanges{Approvals: 2, Files: 4, Added: 50, Removed: 7}) {
		t.Errorf("the first day = %+v", day)
	}
	none, _ := s.CodeChanges(bg, at.Add(72*time.Hour), time.Time{})
	if none != (CodeChanges{}) {
		t.Errorf("an empty period = %+v", none)
	}

	a := domain.NewTaskAggregate(domain.Task{ID: "t9", State: domain.TaskRunning})
	a.AddEnvironment(domain.Environment{ID: "e1", Backend: "apple", State: domain.EnvRunning})
	if err := a.StartRun(domain.Run{ID: "r1", EnvID: "e1"}); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkRunning("r1"); err != nil {
		t.Fatal(err)
	}
	if err := a.StopRun("r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PinPreparedStat("r1", "agent/topic", "aaa111", "src", domain.DiffStat{Files: 2, Added: 5, Removed: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}
	back, err := s.LoadTask(bg, "t9")
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := back.CurrentCandidate(); !ok || c.Files != 2 || c.Added != 5 || c.Removed != 1 {
		t.Errorf("the candidate came back as %+v", c)
	}
}
