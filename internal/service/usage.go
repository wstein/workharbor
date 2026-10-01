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
	rec := domain.UsageRecorded{RunID: run, Repo: agg.Task().Repo, Agent: s.ag.Name(), Model: e.Usage.Model}
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
	rows, err := s.store.UsageTotals(ctx, store.UsageFilter{TaskID: q.TaskID, Repo: q.Repo, Since: q.Since, Until: q.Until}, q.Group)
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
		rep.Rows = append(rep.Rows, UsageRow{UsageRow: r, Notional: r.Auth == string(agent.AuthSubscription)})
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
		in += r.Tokens.Input + r.Tokens.CacheRead + r.Tokens.CacheWrite
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
