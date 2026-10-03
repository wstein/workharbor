package domain

import (
	"encoding/json"
	"math"
	"time"
)

// EventUsage is the audit entry of one turn's usage (design §5.7). It is in the
// audit tier, so a purge of the transcript never changes a total.
const EventUsage EventKind = "usage.recorded"

// CostSource says where a cost figure comes from. A reported cost is the
// agent's own figure. workharbor does not price tokens itself, because a price
// table in the supervisor goes stale, so there is no other source.
type CostSource string

// CostReported is the agent's own figure.
const CostReported CostSource = "reported"

// CostEstimated labels a figure the supervisor or the agent worked out rather than
// read from a bill. Nothing records one yet (#48); the summary already keeps it apart
// so that an estimate is never shown as reported.
const CostEstimated = "estimated"

// UsageTokens are the tokens of one turn.
type UsageTokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// UsageCost is an amount in millionths of a US dollar, so no float holds money,
// and where it comes from.
type UsageCost struct {
	MicroUSD int64      `json:"micro_usd"`
	Source   CostSource `json:"source"`
}

// UsageBalance is what the account has left as of a turn, in millionths of a US
// dollar, as the agent reports it.
type UsageBalance struct {
	RemainingMicroUSD int64 `json:"remaining_micro_usd"`
}

// UsageWindow is how much of a rolling usage limit is used, as of a turn.
type UsageWindow struct {
	Name        string    `json:"name"`
	Utilization float64   `json:"utilization"` // 0 to 1
	ResetsAt    time.Time `json:"resets_at,omitzero"`
}

// UsageRecorded is the payload of EventUsage. Tokens and Cost are nil when the
// agent did not report them, which is not zero: a budget must not read an
// unknown count as no tokens. Auth is the mode the run used, because the same
// cost means real spend with an API key and nothing extra on a subscription.
type UsageRecorded struct {
	RunID   ID            `json:"run_id"`
	Repo    string        `json:"repo,omitempty"`
	Agent   string        `json:"agent"`
	Auth    string        `json:"auth"`
	Model   string        `json:"model"`
	Tokens  *UsageTokens  `json:"tokens,omitempty"`
	Cost    *UsageCost    `json:"cost,omitempty"`
	Balance *UsageBalance `json:"balance,omitempty"`
	Windows []UsageWindow `json:"windows,omitempty"`
	// APIMillis and WallMillis are the turn's time on the model API and its wall
	// time, as the agent reported them (milliseconds), or zero.
	APIMillis  int64 `json:"api_ms,omitempty"`
	WallMillis int64 `json:"wall_ms,omitempty"`
}

// Usage errors.
var (
	ErrUsageRun   = invalid("a usage record needs a task, a run, an agent and a model")
	ErrUsageValue = invalid("a usage record has a negative count or cost, or a utilization outside 0 to 1")
	ErrUsageCost  = invalid("a cost needs the source reported")
)

// NewUsageEvent checks a usage record and returns its audit event.
func NewUsageEvent(task ID, u UsageRecorded, at time.Time) (Event, error) {
	if task == "" || u.RunID == "" || u.Agent == "" || u.Model == "" {
		return Event{}, ErrUsageRun
	}
	if t := u.Tokens; t != nil && (t.Input < 0 || t.Output < 0 || t.CacheRead < 0 || t.CacheWrite < 0) {
		return Event{}, ErrUsageValue
	}
	if c := u.Cost; c != nil {
		if c.MicroUSD < 0 {
			return Event{}, ErrUsageValue
		}
		if c.Source != CostReported {
			return Event{}, ErrUsageCost
		}
	}
	if b := u.Balance; b != nil && b.RemainingMicroUSD < 0 {
		return Event{}, ErrUsageValue
	}
	for _, w := range u.Windows {
		if w.Name == "" || w.Utilization < 0 || w.Utilization > 1 {
			return Event{}, ErrUsageValue
		}
	}
	payload, err := json.Marshal(u)
	if err != nil {
		return Event{}, err
	}
	return Event{TaskID: task, Kind: EventUsage, Tier: TierAudit, Payload: payload, At: at}, nil
}

// SatAdd adds usage amounts and saturates at the int64 limits instead of
// wrapping, so a total never turns negative (design §5.7).
func SatAdd(vs ...int64) int64 {
	var sum int64
	for _, v := range vs {
		switch {
		case v > 0 && sum > math.MaxInt64-v:
			return math.MaxInt64
		case v < 0 && sum < math.MinInt64-v:
			return math.MinInt64
		}
		sum += v
	}
	return sum
}
