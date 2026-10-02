package agent

import (
	"errors"
	"fmt"
	"time"
)

// CostSource says where a cost figure comes from (design §5.7). workharbor
// never prices tokens itself, because a price table in the supervisor goes
// stale: a cost exists only when the agent reports it.
type CostSource string

const (
	// CostReported is the agent's own figure.
	CostReported CostSource = "reported"
)

// Names of the usage windows Claude Code reports (spike #1).
const (
	WindowFiveHour = "five_hour"
	WindowSevenDay = "seven_day"
)

// Balance is what the account has left, as the agent reports it with a turn,
// in millionths of a US dollar. It is streamed like the token counts and is
// nil when the agent reports none, which is not zero.
type Balance struct {
	RemainingMicroUSD int64 `json:"remaining_micro_usd"`
}

// Cost is an amount and where it comes from. In api-key mode it is real
// spend; in subscription mode it is notional (design §5.7).
type Cost struct {
	MicroUSD int64      `json:"micro_usd"` // millionths of a US dollar, so that no float holds money
	Source   CostSource `json:"source"`
}

// UsageWindow is how much of a rolling usage limit has been used.
type UsageWindow struct {
	Name        string    `json:"name"`        // for example WindowFiveHour
	Utilization float64   `json:"utilization"` // 0 to 1
	ResetsAt    time.Time `json:"resets_at,omitzero"`
}

// TokenCounts are the tokens of one turn.
type TokenCounts struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Usage is the payload of a usage event: one turn's tokens and cost, and the
// usage windows when the agent reports them. Tokens and Cost are nil when the
// agent did not report them, which is not the same as zero: a budget must not
// read an unknown count as 0 tokens (design §5.7).
type Usage struct {
	Model   string        `json:"model"`
	Tokens  *TokenCounts  `json:"tokens,omitempty"`
	Cost    *Cost         `json:"cost,omitempty"`
	Balance *Balance      `json:"balance,omitempty"`
	Windows []UsageWindow `json:"windows,omitempty"`
	// APIMillis and WallMillis are the time the agent says the turn spent waiting on
	// the model API and its wall time, in milliseconds; zero when it reports none.
	APIMillis  int64 `json:"api_ms,omitempty"`
	WallMillis int64 `json:"wall_ms,omitempty"`
}

// ErrBadUsage reports a usage payload that cannot be recorded.
var ErrBadUsage = errors.New("usage payload is not well formed")

// Validate checks that a payload can be recorded: a model, no negative
// counts, a cost with a known source, and windows with a name and a
// utilization from 0 to 1.
func (u Usage) Validate() error {
	if u.Model == "" {
		return fmt.Errorf("%w: no model", ErrBadUsage)
	}
	if t := u.Tokens; t != nil && (t.Input < 0 || t.Output < 0 || t.CacheRead < 0 || t.CacheWrite < 0) {
		return fmt.Errorf("%w: a token count is negative", ErrBadUsage)
	}
	if u.Cost != nil {
		if u.Cost.MicroUSD < 0 {
			return fmt.Errorf("%w: a negative cost", ErrBadUsage)
		}
		if u.Cost.Source != CostReported {
			return fmt.Errorf("%w: cost source %q is not reported", ErrBadUsage, u.Cost.Source)
		}
	}
	if u.APIMillis < 0 || u.WallMillis < 0 {
		return fmt.Errorf("%w: a negative duration", ErrBadUsage)
	}
	if u.Balance != nil && u.Balance.RemainingMicroUSD < 0 {
		return fmt.Errorf("%w: a negative balance", ErrBadUsage)
	}
	for _, w := range u.Windows {
		if w.Name == "" {
			return fmt.Errorf("%w: a usage window has no name", ErrBadUsage)
		}
		if w.Utilization < 0 || w.Utilization > 1 {
			return fmt.Errorf("%w: window %s has utilization %v, want 0 to 1", ErrBadUsage, w.Name, w.Utilization)
		}
	}
	return nil
}
