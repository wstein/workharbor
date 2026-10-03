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

// maxLimitWarned bounds the window keys of limitWarned: window names come from
// the agent. The balance key is outside the cap.
const maxLimitWarned = 64

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

// warnLowLimits pushes when a reported window or balance turns low: the same
// signal the dashboard shows. A key warns again only after a reading below the
// threshold and once limitWarnCooldown has passed since its last push. It runs
// after a usage report was recorded and only looks at what that report carried.
func (s *Service) warnLowLimits(task domain.ID, u *agent.Usage) {
	if s.cfg.Notifier == nil {
		return
	}
	low := map[string]bool{} // key -> low in this report
	now := s.clock.Now()
	limit := float64(s.cfg.LowLimits.windowPercent()) / 100
	for _, w := range u.Windows {
		// The key is the window name alone: the reset time is the agent's
		// own report and must not mint new keys.
		low["window/"+w.Name] = w.Utilization >= limit && !resetPassed(w.ResetsAt, now)
	}
	if b := u.Balance; b != nil {
		low["balance"] = s.cfg.LowLimits.BalanceMicroUSD > 0 && b.RemainingMicroUSD <= s.cfg.LowLimits.BalanceMicroUSD
	}
	s.mu.Lock()
	if s.limitWarned == nil {
		s.limitWarned = map[string]limitWarn{}
	}
	windows := 0
	for k := range s.limitWarned {
		if k != "balance" {
			windows++
		}
	}
	fresh := false
	for k, isLow := range low {
		e, known := s.limitWarned[k]
		if !known && k != "balance" && windows >= maxLimitWarned {
			// At the cap: forget a window this report does not mention.
			for old := range s.limitWarned {
				if _, inReport := low[old]; old != "balance" && !inReport {
					delete(s.limitWarned, old)
					windows--
					break
				}
			}
			if windows >= maxLimitWarned {
				continue
			}
		}
		if !known && k != "balance" {
			windows++
		}
		wasLow := e.low
		e.low = isLow
		if isLow && !wasLow && (e.at.IsZero() || now.Sub(e.at) >= limitWarnCooldown) {
			e.at, fresh = now, true
		}
		s.limitWarned[k] = e
	}
	s.mu.Unlock()
	if fresh {
		s.report(s.cfg.Notifier.Notify(context.Background(), notify.Message{TaskID: task, Kind: notify.KindLimitLow}))
	}
}
