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

// maxLimitWarned bounds limitWarned: window names come from the agent.
const maxLimitWarned = 64

func resetPassed(resetsAt, now time.Time) bool { return !resetsAt.IsZero() && now.After(resetsAt) }

// warnLowLimits notifies once per window reading period when a reported window
// or balance is low: the same signal the dashboard shows. It runs after a usage
// report was recorded and only looks at what that report carried.
func (s *Service) warnLowLimits(task domain.ID, u *agent.Usage) {
	if s.cfg.Notifier == nil {
		return
	}
	var keys, recovered []string
	now := s.clock.Now()
	limit := float64(s.cfg.LowLimits.windowPercent()) / 100
	for _, w := range u.Windows {
		// The key is the window name alone: the reset time is the agent's
		// own report and must not mint new keys.
		k := "window/" + w.Name
		if w.Utilization >= limit && !resetPassed(w.ResetsAt, now) {
			keys = append(keys, k)
		} else {
			recovered = append(recovered, k)
		}
	}
	if b := u.Balance; b != nil && s.cfg.LowLimits.BalanceMicroUSD > 0 && b.RemainingMicroUSD <= s.cfg.LowLimits.BalanceMicroUSD {
		keys = append(keys, "balance")
	}
	s.mu.Lock()
	if s.limitWarned == nil {
		s.limitWarned = map[string]bool{}
	}
	fresh := false
	for _, k := range recovered {
		delete(s.limitWarned, k)
	}
	for _, k := range keys {
		if !s.limitWarned[k] && len(s.limitWarned) < maxLimitWarned {
			s.limitWarned[k], fresh = true, true
		}
	}
	// A balance that recovered may warn again.
	if u.Balance != nil && (s.cfg.LowLimits.BalanceMicroUSD <= 0 || u.Balance.RemainingMicroUSD > s.cfg.LowLimits.BalanceMicroUSD) {
		delete(s.limitWarned, "balance")
	}
	s.mu.Unlock()
	if fresh {
		s.report(s.cfg.Notifier.Notify(context.Background(), notify.Message{TaskID: task, Kind: notify.KindLimitLow}))
	}
}
