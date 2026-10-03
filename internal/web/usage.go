package web

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/api"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

// windowRow is one usage window of the account, as a meter.
type windowRow struct {
	ID      string // a valid element id: "five-hour"
	Name    string // "five hour", "seven day"
	Percent int    // 0 to 100, as the agent reported it
	Resets  string // "resets in 2h 10m", or empty
}

// usagePeriods are the periods of the card, in the order of its links.
var usagePeriods = []struct{ Key, Label string }{
	{service.PeriodToday, "Today"}, {service.Period7Days, "7 days"}, {service.Period30Days, "30 days"}, {service.PeriodAll, "All"},
}

// usageSorts are the columns a breakdown can be sorted by.
var usageSorts = map[string]func(a, b service.UsageRow) bool{
	"cost": func(a, b service.UsageRow) bool {
		return a.ReportedMicroUSD+a.EstimatedMicroUSD > b.ReportedMicroUSD+b.EstimatedMicroUSD
	},
	"tokens": func(a, b service.UsageRow) bool { return rowTokens(a) > rowTokens(b) },
	"turns":  func(a, b service.UsageRow) bool { return a.Turns > b.Turns },
	"cache": func(a, b service.UsageRow) bool {
		return share(a) > share(b)
	},
	"name": func(a, b service.UsageRow) bool { return a.Key < b.Key },
}

func rowTokens(r service.UsageRow) int64 {
	return domain.SatAdd(r.Tokens.Input, r.Tokens.Output, r.Tokens.CacheRead, r.Tokens.CacheWrite)
}

func share(r service.UsageRow) float64 {
	if r.CacheShare == nil {
		return -1
	}
	return *r.CacheShare
}

// duration writes milliseconds as 1h02m, 3m04s or 12s, and "-" for none.
func duration(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// costText says what the cost is, never calling an estimate reported, and an
// API-key figure real spend while a subscription's is API-equivalent and not billed.
func costText(r service.UsageRow) (cost, label string) {
	label = r.CostLabel
	switch r.CostLabel {
	case "none":
		return "not reported", label
	case "estimated":
		cost = service.FormatMicroUSD(r.EstimatedMicroUSD)
	case "mixed":
		cost = service.FormatMicroUSD(r.ReportedMicroUSD) + " + " + service.FormatMicroUSD(r.EstimatedMicroUSD)
	default:
		cost = service.FormatMicroUSD(r.ReportedMicroUSD)
	}
	if r.Notional {
		label += ", API-equivalent, not billed"
	} else if r.Auth == "api-key" {
		label += ", spend"
	}
	return cost, label
}

func usageRowOf(r service.UsageRow, key string, link string) usageRowView {
	cost, label := costText(r)
	cache := "-"
	if r.CacheShare != nil {
		cache = fmt.Sprintf("%.0f%%", *r.CacheShare*100)
	}
	in := service.Compact(r.Tokens.Input)
	out := service.Compact(r.Tokens.Output)
	if r.TurnsWithoutToken == r.Turns {
		in, out = "-", "-"
	}
	return usageRowView{
		Key: key, Auth: r.Auth, Turns: fmt.Sprint(r.Turns), Runs: fmt.Sprint(r.Runs), In: in, Out: out,
		CacheRead: service.Compact(r.Tokens.CacheRead), CacheWrite: service.Compact(r.Tokens.CacheWrite), Cache: cache,
		Cost: cost, Label: label, APITime: duration(r.APIMillis), WallTime: duration(r.WallMillis), Link: link,
	}
}

func rowsOf(rows []service.UsageRow, sortBy string, keyOf func(string) (string, string)) []usageRowView {
	rows = append([]service.UsageRow(nil), rows...)
	if less, ok := usageSorts[sortBy]; ok {
		sort.SliceStable(rows, func(i, j int) bool { return less(rows[i], rows[j]) })
	}
	out := make([]usageRowView, len(rows))
	for i, r := range rows {
		key, link := keyOf(r.Key)
		out[i] = usageRowOf(r, key, link)
	}
	return out
}

// usageCardOf builds the card for a period from the service's summary: no logic
// of its own beyond ordering and wording (D8).
func (s *Server) usageCardOf(ctx context.Context, period, sortBy string) *usageCard {
	sum, ok := s.be.(api.UsageSummarizer)
	if !ok {
		return nil
	}
	valid := false
	for _, p := range usagePeriods {
		valid = valid || p.Key == period
	}
	if !valid {
		period = service.Period7Days
	}
	rep, err := sum.UsageSummary(ctx, period)
	if err != nil {
		if s.opt.OnError != nil {
			s.opt.OnError(err)
		}
		return nil
	}
	if _, ok := usageSorts[sortBy]; !ok {
		sortBy = "cost"
	}
	c := &usageCard{Period: period, Subscription: rep.Subscription, Sort: sortBy, SortLinks: map[string]string{}}
	for _, p := range usagePeriods {
		c.Periods = append(c.Periods, usagePeriodLink{Label: p.Label, Href: "/?period=" + p.Key, Current: p.Key == period})
	}
	for k := range usageSorts {
		c.SortLinks[k] = "/?period=" + period + "&sort=" + k
	}
	if rep.Subscription { // the usage window is what limits the human: it leads
		for _, w := range rep.Windows {
			pct := min(max(int(w.Utilization*100+0.5), 0), 100)
			row := windowRow{ID: idOf(w.Name), Name: strings.ReplaceAll(w.Name, "_", " "), Percent: pct}
			if !w.ResetsAt.IsZero() {
				if d := w.ResetsAt.Sub(s.opt.Now()); d > 0 {
					row.Resets = "resets in " + shortDuration(d)
				} else {
					row.Resets = "reset"
				}
			}
			c.Windows = append(c.Windows, row)
		}
	}
	if n := rep.Code; n.Approvals > 0 {
		c.Code = fmt.Sprintf("%d approved commit set(s): %d file(s), +%d −%d lines", n.Approvals, n.Files, n.Added, n.Removed)
	}
	if rep.Balance != nil {
		c.Balance = service.FormatMicroUSD(rep.Balance.RemainingMicroUSD) + " left, as the agent reported"
	}
	for _, r := range rep.Total {
		c.Total = append(c.Total, usageRowOf(r, "All agents", ""))
	}
	c.ByAgent = rowsOf(rep.ByAgent, sortBy, func(k string) (string, string) {
		if k == "" {
			return "(no agent)", ""
		}
		return k, "/?agent=" + url.QueryEscape(k) + "&period=" + period
	})
	c.ByModel = rowsOf(rep.ByModel, sortBy, func(k string) (string, string) { return k, "" })
	return c
}

// shortDuration writes a duration as "2h 10m" or "12m", rounded to the minute.
func shortDuration(d time.Duration) string {
	m := int((d + 30*time.Second) / time.Minute)
	switch {
	case m >= 24*60:
		return fmt.Sprintf("%dd %dh", m/(24*60), m%(24*60)/60)
	case m >= 60:
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return fmt.Sprintf("%dm", max(m, 1))
}

// taskUsage is the usage line of a task: its turns, tokens and cost, with the
// account's windows in front on a subscription (service.FormatUsageLine).
func (s *Server) taskUsage(ctx context.Context, task string) string {
	rep, err := s.be.Usage(ctx, service.UsageQuery{TaskID: domain.ID(task), Group: store.GroupTask})
	if err != nil {
		s.report(fmt.Errorf("usage: %w", err))
		return ""
	}
	return service.FormatUsageLine(rep)
}

// report hands an error to the operator's hook, if there is one.
func (s *Server) report(err error) {
	if s.opt.OnError != nil {
		s.opt.OnError(err)
	}
}

// idOf makes a window's name an element id: lower case letters, digits and hyphens.
func idOf(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// limitsOf builds the limits panel from the service's report: only what the
// adapters reported, `unknown` for the rest (D40), no logic of its own.
func (s *Server) limitsOf(ctx context.Context) []limitView {
	lr, ok := s.be.(api.LimitsReporter)
	if !ok {
		return nil
	}
	ps, err := lr.Limits(ctx)
	if err != nil {
		s.report(fmt.Errorf("limits: %w", err))
		return nil
	}
	now := s.opt.Now()
	age := func(at time.Time) string {
		if d := now.Sub(at); d > time.Minute {
			return shortDuration(d)
		}
		return "under a minute"
	}
	var out []limitView
	for _, p := range ps {
		v := limitView{Provider: p.Provider, Source: p.Source, Unknown: !p.Known, Low: p.Low}
		if p.BudgetMicroUSD > 0 {
			v.Budget = service.FormatMicroUSD(p.BudgetMicroUSD) + " per task (API key)"
		}
		for _, w := range p.Windows {
			used := min(max(int(w.Used*100+0.5), 0), 100)
			row := limitWindowView{
				ID: idOf(w.Name), Name: strings.ReplaceAll(w.Name, "_", " "), Percent: used, Low: w.Low,
				Used: fmt.Sprintf("%d%%", used), Left: fmt.Sprintf("%d%%", 100-used), Age: age(w.At),
			}
			if !w.ResetsAt.IsZero() {
				if d := w.ResetsAt.Sub(now); d > 0 {
					row.Resets = "resets in " + shortDuration(d)
				} else {
					row.Resets = "reset"
				}
			}
			v.Windows = append(v.Windows, row)
		}
		if p.Balance != nil {
			v.Balance = service.FormatMicroUSD(p.Balance.RemainingMicroUSD) + " left, read " + age(p.Balance.At) + " ago"
		}
		out = append(out, v)
	}
	return out
}
