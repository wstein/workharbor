package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

// usageBox is what the harbor page shows of usage (design §5.7, D40): the
// account's own usage windows lead, because on a subscription they are the number
// that matters and the cost is only notional; the totals of the last day follow,
// and a balance the agent reported.
type usageBox struct {
	Windows []windowRow
	Totals  string // turns, tokens and the agent's reported cost over the last day
	Balance string
}

// windowRow is one usage window of the account, as a meter.
type windowRow struct {
	ID      string // a valid element id: "five-hour"
	Name    string // "five hour", "seven day"
	Percent int    // 0 to 100, as the agent reported it
	Resets  string // "resets in 2h 10m", or empty
}

// usageWindow is how far back the harbor totals reach.
const usageWindow = 24 * time.Hour

// usageBoxOf builds the box from a report of the last day. It returns nil when
// there is nothing to show, so a fresh install shows no empty panel.
func usageBoxOf(rep service.UsageReport, now time.Time) *usageBox {
	box := &usageBox{}
	for _, w := range rep.Windows {
		pct := int(w.Utilization*100 + 0.5)
		pct = min(max(pct, 0), 100)
		row := windowRow{ID: idOf(w.Name), Name: strings.ReplaceAll(w.Name, "_", " "), Percent: pct}
		if !w.ResetsAt.IsZero() {
			if d := w.ResetsAt.Sub(now); d > 0 {
				row.Resets = "resets in " + shortDuration(d)
			} else {
				row.Resets = "reset"
			}
		}
		box.Windows = append(box.Windows, row)
	}
	var turns, in, out, unknown, reported int64
	notional := false
	for _, r := range rep.Rows {
		turns += r.Turns
		in += r.Tokens.Input + r.Tokens.CacheRead + r.Tokens.CacheWrite
		out += r.Tokens.Output
		unknown += r.TurnsWithoutCost
		reported += r.ReportedMicroUSD
		notional = notional || r.Notional
	}
	if turns > 0 {
		cost := "cost not reported"
		if reported > 0 || unknown == 0 {
			cost = service.FormatMicroUSD(reported) + " reported"
		}
		if notional {
			cost += ", notional (subscription)"
		}
		box.Totals = fmt.Sprintf("Last 24 hours: %d turns, %s in, %s out; %s", turns, service.Compact(in), service.Compact(out), cost)
	}
	if rep.Balance != nil {
		box.Balance = service.FormatMicroUSD(rep.Balance.RemainingMicroUSD) + " left, as the agent reported"
	}
	if len(box.Windows) == 0 && box.Totals == "" && box.Balance == "" {
		return nil
	}
	return box
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

// harborUsage reads the account's usage for the harbor page. A usage that cannot
// be read leaves the panel out and is reported: the page is for the tasks.
func (s *Server) harborUsage(ctx context.Context) *usageBox {
	rep, err := s.be.Usage(ctx, service.UsageQuery{Since: s.opt.Now().Add(-usageWindow), Group: store.GroupAll})
	if err != nil {
		s.report(fmt.Errorf("usage: %w", err))
		return nil
	}
	return usageBoxOf(rep, s.opt.Now())
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
