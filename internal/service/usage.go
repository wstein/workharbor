package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/store"
)

// recordUsage turns an agent's usage report into an audit entry (design §5.7)
// and keeps the usage windows it carries. Tokens, cost, balance and windows are
// the agent's own figures, stored as it reported them: a figure the agent did
// not report is absent, and workharbor never prices tokens itself. A report
// that is not well formed is reported and dropped: the run goes on.
func (s *Service) recordUsage(ctx context.Context, task, run domain.ID, e agent.Event) {
	if e.Usage == nil {
		return
	}
	if err := e.Usage.Validate(); err != nil {
		s.report(fmt.Errorf("usage event: %w", err))
		return
	}
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		s.report(fmt.Errorf("usage event: %w", err))
		return
	}
	rec := domain.UsageRecorded{RunID: run, Repo: agg.Task().Repo, Agent: s.ag.Name(), Model: e.Usage.Model, APIMillis: e.Usage.APIMillis, WallMillis: e.Usage.WallMillis}
	if r, ok := agg.Run(run); ok && s.cfg.Spec != nil {
		rec.Auth = string(s.cfg.Spec(agg.Task(), r).Auth)
	}
	if rec.Auth == "" {
		rec.Auth = "unknown"
	}
	if t := e.Usage.Tokens; t != nil {
		rec.Tokens = &domain.UsageTokens{Input: t.Input, Output: t.Output, CacheRead: t.CacheRead, CacheWrite: t.CacheWrite}
	}
	if c := e.Usage.Cost; c != nil {
		rec.Cost = &domain.UsageCost{MicroUSD: c.MicroUSD, Source: domain.CostSource(c.Source)}
	}
	if b := e.Usage.Balance; b != nil {
		rec.Balance = &domain.UsageBalance{RemainingMicroUSD: b.RemainingMicroUSD}
	}
	for _, w := range e.Usage.Windows {
		rec.Windows = append(rec.Windows, domain.UsageWindow{Name: w.Name, Utilization: w.Utilization, ResetsAt: w.ResetsAt})
	}
	at := e.At
	if at.IsZero() {
		at = s.clock.Now()
	}
	ev, err := domain.NewUsageEvent(task, rec, at)
	if err != nil {
		s.report(fmt.Errorf("usage event: %w", err))
		return
	}
	saved, err := s.store.AppendUsage(ctx, s.ag.Name(), ev)
	if err != nil {
		s.report(fmt.Errorf("usage event: %w", err))
		return
	}
	s.publish([]domain.Event{saved})
	s.checkBudgets(ctx, task, run)
}

// UsageQuery selects what a usage report totals.
type UsageQuery struct {
	TaskID domain.ID
	Repo   string
	Since  time.Time // inclusive; zero for everything
	Until  time.Time // exclusive; zero for now
	Group  store.UsageGroup
}

// UsageRow is one row of a usage report. Notional is true for a subscription:
// the plan is paid flat, so the cost is what the turns would have cost and the
// usage window is the number that limits the developer (design §5.2). A
// turn without tokens or without a cost is counted, not read as zero.
type UsageRow struct {
	store.UsageRow
	Notional bool `json:"notional"`
	// CacheShare is the share of input tokens served from the cache, 0 to 1, or nil
	// when the turns reported no input tokens.
	CacheShare *float64 `json:"cache_share,omitempty"`
	// CostLabel says what the cost figure is: reported, estimated, mixed (both) or
	// none (no turn reported one). An estimate is never shown as reported.
	CostLabel string `json:"cost_label"`
}

// cacheShare is cache_read over every input token (fresh, cache read and cache write).
func cacheShare(t domain.UsageTokens) *float64 {
	total := domain.SatAdd(t.Input, t.CacheRead, t.CacheWrite)
	if total <= 0 {
		return nil
	}
	v := float64(t.CacheRead) / float64(total)
	return &v
}

func costLabel(r store.UsageRow) string {
	switch {
	case r.ReportedMicroUSD > 0 && r.EstimatedMicroUSD > 0:
		return "mixed"
	case r.EstimatedMicroUSD > 0:
		return domain.CostEstimated
	case r.ReportedMicroUSD > 0 || r.TurnsWithoutCost < r.Turns:
		return string(domain.CostReported)
	}
	return "none"
}

func (s *Service) location() *time.Location {
	if s.cfg.Location != nil {
		return s.cfg.Location
	}
	return time.Local
}

// UsageReport is what `whr usage` and the UI show. Windows are the account's
// own figures, one per window across every run (D40), and lead in subscription
// mode.
type UsageReport struct {
	Group   store.UsageGroup      `json:"group"`
	Since   time.Time             `json:"since,omitzero"`
	Until   time.Time             `json:"until,omitzero"`
	Rows    []UsageRow            `json:"rows"`
	Windows []store.WindowReading `json:"windows"`
	// Balance is the latest balance the agent reported, or nil.
	Balance *store.BalanceReading `json:"balance,omitempty"`
}

// Usage totals usage by group. It reads the audit entries, so it is the same
// after a transcript purge, and the budgets of §7.4 read the same counters
// through store.UsageTotals.
func (s *Service) Usage(ctx context.Context, q UsageQuery) (UsageReport, error) {
	if q.Group == "" {
		q.Group = store.GroupTask
	}
	f := store.UsageFilter{TaskID: q.TaskID, Repo: q.Repo, Since: q.Since, Until: q.Until}
	var rows []store.UsageRow
	var err error
	switch q.Group {
	case store.GroupDay:
		rows, err = s.store.UsageBuckets(ctx, f, s.location(), "day")
	case store.GroupMonth:
		rows, err = s.store.UsageBuckets(ctx, f, s.location(), "month")
	default:
		rows, err = s.store.UsageTotals(ctx, f, q.Group)
	}
	if err != nil {
		return UsageReport{}, err
	}
	windows, err := s.store.UsageWindows(ctx, s.ag.Name())
	if err != nil {
		return UsageReport{}, err
	}
	balance, err := s.store.LatestBalance(ctx, s.ag.Name())
	if err != nil {
		return UsageReport{}, err
	}
	rep := UsageReport{Balance: balance, Group: q.Group, Since: q.Since, Until: q.Until, Rows: make([]UsageRow, 0, len(rows)), Windows: windows}
	if rep.Windows == nil {
		rep.Windows = []store.WindowReading{}
	}
	for _, r := range rows {
		rep.Rows = append(rep.Rows, UsageRow{UsageRow: r, Notional: r.Auth == string(agent.AuthSubscription), CacheShare: cacheShare(r.Tokens), CostLabel: costLabel(r)})
	}
	return rep, nil
}

// UsageLine is the usage line of `whr show` for a task, or "" when it has none.
func (s *Service) UsageLine(ctx context.Context, task domain.ID) (string, error) {
	rep, err := s.Usage(ctx, UsageQuery{TaskID: task, Group: store.GroupTask})
	if err != nil {
		return "", err
	}
	return FormatUsageLine(rep), nil
}

// FormatUsageLine writes a report as one line. In subscription mode the window
// comes first and the cost is called notional; a cost is always the agent's
// reported figure, and a balance is shown when the agent reported one.
func FormatUsageLine(rep UsageReport) string {
	if len(rep.Rows) == 0 {
		return ""
	}
	var turns, in, out, unknown int64
	var reported int64
	subscription := false
	for _, r := range rep.Rows {
		turns += r.Turns
		in = domain.SatAdd(in, r.Tokens.Input, r.Tokens.CacheRead, r.Tokens.CacheWrite)
		out += r.Tokens.Output
		unknown += r.TurnsWithoutCost
		reported += r.ReportedMicroUSD
		subscription = subscription || r.Notional
	}
	var parts []string
	if subscription {
		for _, w := range rep.Windows {
			parts = append(parts, fmt.Sprintf("%s %.0f%%", strings.ReplaceAll(w.Name, "_", "-"), w.Utilization*100))
		}
	}
	parts = append(parts, fmt.Sprintf("%d turns, %s in, %s out", turns, Compact(in), Compact(out)))
	c := "cost not reported"
	if reported > 0 || unknown == 0 {
		c = FormatMicroUSD(reported) + " reported"
	}
	if subscription {
		c += ", notional (subscription)"
	}
	if rep.Balance != nil {
		c += "; balance " + FormatMicroUSD(rep.Balance.RemainingMicroUSD) + " left"
	}
	return "usage: " + strings.Join(parts, "; ") + "; " + c
}

// FormatMicroUSD writes millionths of a dollar as dollars with four decimals.
func FormatMicroUSD(micro int64) string {
	t := (micro + 50) / 100 // ten-thousandths of a dollar, rounded
	return fmt.Sprintf("$%d.%04d", t/10_000, t%10_000)
}

// Compact writes a token count as 1.2k or 3.4M.
func Compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// Periods of the usage summary.
const (
	PeriodToday  = "today"
	Period7Days  = "7d"
	Period30Days = "30d"
	PeriodAll    = "all"
)

// UsageSummary is the dashboard's usage card and `whr usage --period`: what every agent
// used over a period, in total, by agent and by model. The same Rows as Usage, from
// the same method, so the page and the command cannot disagree.
type UsageSummary struct {
	Period string    `json:"period"`
	Since  time.Time `json:"since,omitzero"`
	Until  time.Time `json:"until"`
	// Total is one row per auth mode: a subscription's notional cost is never added to
	// an API key's spend.
	Total   []UsageRow            `json:"total"`
	ByAgent []UsageRow            `json:"by_agent"`
	ByModel []UsageRow            `json:"by_model"`
	Windows []store.WindowReading `json:"windows"`
	Balance *store.BalanceReading `json:"balance,omitempty"`
	// Code is the size of the commits approved in the period: approvals, files and the
	// lines added and removed, from the review.approved audit entries.
	Code store.CodeChanges `json:"code"`
	// Subscription says some turns ran on a subscription: the windows lead, and the
	// cost is API-equivalent, not billed.
	Subscription bool `json:"subscription"`
}

// PeriodStart returns the start of a period in the supervisor's time zone: midnight
// today, six days before it (seven days with today), 29 days before it, or the zero
// time for all. The boundary is the supervisor's midnight, not UTC's.
func (s *Service) PeriodStart(period string, now time.Time) (time.Time, error) {
	loc := s.location()
	y, m, d := now.In(loc).Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch period {
	case PeriodToday:
		return midnight, nil
	case Period7Days:
		return midnight.AddDate(0, 0, -6), nil
	case Period30Days:
		return midnight.AddDate(0, 0, -29), nil
	case PeriodAll, "":
		return time.Time{}, nil
	}
	return time.Time{}, &domain.InvalidError{Msg: "the period is today, 7d, 30d or all"}
}

// UsageSummary builds the card for a period ending now.
func (s *Service) UsageSummary(ctx context.Context, period string, now time.Time) (UsageSummary, error) {
	since, err := s.PeriodStart(period, now)
	if err != nil {
		return UsageSummary{}, err
	}
	if period == "" {
		period = PeriodAll
	}
	sum := UsageSummary{Period: period, Since: since, Until: now}
	for _, g := range []struct {
		group store.UsageGroup
		into  *[]UsageRow
	}{{store.GroupAll, &sum.Total}, {store.GroupAgent, &sum.ByAgent}, {store.GroupModel, &sum.ByModel}} {
		rep, err := s.Usage(ctx, UsageQuery{Since: since, Until: now.Add(time.Nanosecond), Group: g.group})
		if err != nil {
			return UsageSummary{}, err
		}
		*g.into = rep.Rows
		if g.group == store.GroupAll {
			sum.Windows, sum.Balance = rep.Windows, rep.Balance
		}
	}
	for _, r := range sum.Total {
		sum.Subscription = sum.Subscription || r.Notional
	}
	if sum.Code, err = s.store.CodeChanges(ctx, since, now.Add(time.Nanosecond)); err != nil {
		return UsageSummary{}, err
	}
	return sum, nil
}
