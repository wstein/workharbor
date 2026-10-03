package service

import (
	"context"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/notify"
	"github.com/wstein/workharbor/internal/store"
)

// LowLimits say when a provider's limit counts as low (issue #172). Zero fields
// take the defaults: a usage window at 90 % used, and no balance threshold (a
// balance is not called low until the human sets what low means).
type LowLimits struct {
	WindowPercent   int   // 1 to 99: the share of a window used at which it is low
	BalanceMicroUSD int64 // a balance at or under this is low; zero sets none
}

const defaultLowWindowPercent = 90

func (l LowLimits) windowPercent() int {
	if l.WindowPercent < 1 || l.WindowPercent > 99 {
		return defaultLowWindowPercent
	}
	return l.WindowPercent
}

// LimitWindow is one usage window of a provider as the adapter reported it.
type LimitWindow struct {
	Name     string    `json:"name"`
	Used     float64   `json:"used"` // 0 to 1, as reported
	ResetsAt time.Time `json:"resets_at,omitzero"`
	At       time.Time `json:"at"`
	Low      bool      `json:"low"`
}

// ProviderLimits is what is known about one provider's limits. Known is false
// when nothing was reported: the panel then shows `unknown`, never zero, full
// or an estimate (D40). Every figure is an adapter's own report from inside the
// environment; the supervisor reads no login, credential file or token.
type ProviderLimits struct {
	Provider string `json:"provider"`
	Known    bool   `json:"known"`
	// Source says where the figures come from.
	Source  string                `json:"source"`
	Windows []LimitWindow         `json:"windows"`
	Balance *store.BalanceReading `json:"balance,omitempty"`
	// LowBalance is set when the balance is at or under the configured threshold.
	LowBalance bool `json:"low_balance"`
	// Low is set when any figure is low.
	Low bool `json:"low"`
	// APIKey says the provider ran on an API key; BudgetMicroUSD is then the
	// configured per-task cost budget (D41), zero when none is set.
	APIKey         bool  `json:"api_key"`
	BudgetMicroUSD int64 `json:"budget_micro_usd,omitempty"`
}

// Limits reports the limits of every configured provider, from the adapters'
// usage reports only. It reads the usage audit entries through the store and
// nothing else.
func (s *Service) Limits(ctx context.Context) ([]ProviderLimits, error) {
	name := s.ag.Name()
	windows, err := s.store.UsageWindows(ctx, name)
	if err != nil {
		return nil, err
	}
	balance, err := s.store.LatestBalance(ctx, name)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.UsageTotals(ctx, store.UsageFilter{}, store.GroupAll)
	if err != nil {
		return nil, err
	}
	p := ProviderLimits{Provider: name, Windows: []LimitWindow{}, Balance: balance}
	for _, r := range rows {
		if r.Auth == string(agent.AuthAPIKey) {
			p.APIKey = true
		}
	}
	if p.APIKey {
		p.BudgetMicroUSD = s.cfg.Budgets.PerTask.MaxCostMicroUSD
	}
	limit := float64(s.cfg.LowLimits.windowPercent()) / 100
	now := s.clock.Now()
	for _, w := range windows {
		// A reading is shown as it was read (D40), but not called low once its
		// own reset time has passed.
		lw := LimitWindow{Name: w.Name, Used: w.Utilization, ResetsAt: w.ResetsAt, At: w.At, Low: w.Utilization >= limit && !resetPassed(w.ResetsAt, now)}
		p.Low = p.Low || lw.Low
		p.Windows = append(p.Windows, lw)
	}
	if balance != nil && s.cfg.LowLimits.BalanceMicroUSD > 0 && balance.RemainingMicroUSD <= s.cfg.LowLimits.BalanceMicroUSD {
		p.LowBalance, p.Low = true, true
	}
	p.Known = len(windows) > 0 || balance != nil
	p.Source = "not reported by the " + name + " adapter"
	if p.Known {
		p.Source = "reported by the " + name + " adapter"
	}
	return []ProviderLimits{p}, nil
}

// limitWarnCooldown is the least time between two pushes for one key, so a
// reading that alternates around the threshold cannot push on every low turn.
// A quota window lasts hours (five hours at the shortest), so an hour keeps
// the warning timely after a real recovery without chattering.
const limitWarnCooldown = time.Hour

// limitWarn is what one key last did: when it last pushed and whether its
// latest reading was low.
type limitWarn struct {
	at  time.Time
	low bool
}

func resetPassed(resetsAt, now time.Time) bool { return !resetsAt.IsZero() && now.After(resetsAt) }

// limitKey maps a window name to one of at most three keys (with the balance
// key, four in all): the two known windows get their own, every other name the
// agent invents shares window/other, so the map cannot grow and a flood of
// made-up names cannot evict a real window's cooldown.
func limitKey(name string) string {
	switch name {
	case agent.WindowFiveHour, agent.WindowSevenDay:
		return "window/" + name
	}
	return "window/other"
}

// warnLowLimits pushes when a reported window or balance turns low: the same
// signal the dashboard shows. A key warns again only after a reading below the
// threshold and once limitWarnCooldown has passed since its last push; the low
// state is recorded only when a push happens, so a window that stays low
// through the cooldown warns once it passes. It runs after a usage report was
// recorded and only looks at what that report carried.
func (s *Service) warnLowLimits(task domain.ID, u *agent.Usage) {
	if s.cfg.Notifier == nil {
		return
	}
	low := map[string]bool{} // key -> low in this report
	now := s.clock.Now()
	limit := float64(s.cfg.LowLimits.windowPercent()) / 100
	for _, w := range u.Windows {
		k := limitKey(w.Name)
		low[k] = low[k] || (w.Utilization >= limit && !resetPassed(w.ResetsAt, now))
	}
	if b := u.Balance; b != nil {
		low["balance"] = s.cfg.LowLimits.BalanceMicroUSD > 0 && b.RemainingMicroUSD <= s.cfg.LowLimits.BalanceMicroUSD
	}
	s.mu.Lock()
	if s.limitWarned == nil {
		s.limitWarned = map[string]limitWarn{}
	}
	fresh := false
	for k, isLow := range low {
		e := s.limitWarned[k]
		if !isLow {
			e.low = false
		} else if !e.low && (e.at.IsZero() || now.Sub(e.at) >= limitWarnCooldown) {
			e.low, e.at, fresh = true, now, true
		}
		s.limitWarned[k] = e
	}
	s.mu.Unlock()
	if fresh {
		s.report(s.cfg.Notifier.Notify(context.Background(), notify.Message{TaskID: task, Kind: notify.KindLimitLow}))
	}
}
