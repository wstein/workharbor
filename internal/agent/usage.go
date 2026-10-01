package agent

import (
	"errors"
	"fmt"
	"time"
)

// CostSource says where a cost figure comes from (design §5.7).
type CostSource string

const (
	// CostReported is the agent's own figure.
	CostReported CostSource = "reported"
	// CostEstimated is computed by workharbor from a pinned, dated price table
	// and is labelled as an estimate wherever it is shown.
	CostEstimated CostSource = "estimated"
)

// Names of the usage windows Claude Code reports (spike #1).
const (
	WindowFiveHour = "five_hour"
	WindowSevenDay = "seven_day"
)

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

// Usage is the payload of a usage event: one turn's tokens and cost, and the
// usage windows when the agent reports them.
type Usage struct {
	Model            string        `json:"model"`
	InputTokens      int64         `json:"input_tokens"`
	OutputTokens     int64         `json:"output_tokens"`
	CacheReadTokens  int64         `json:"cache_read_tokens"`
	CacheWriteTokens int64         `json:"cache_write_tokens"`
	Cost             *Cost         `json:"cost,omitempty"` // nil when the agent reports none
	Windows          []UsageWindow `json:"windows,omitempty"`
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
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 {
		return fmt.Errorf("%w: a token count is negative", ErrBadUsage)
	}
	if u.Cost != nil {
		if u.Cost.MicroUSD < 0 {
			return fmt.Errorf("%w: a negative cost", ErrBadUsage)
		}
		if u.Cost.Source != CostReported && u.Cost.Source != CostEstimated {
			return fmt.Errorf("%w: cost source %q is neither reported nor estimated", ErrBadUsage, u.Cost.Source)
		}
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
