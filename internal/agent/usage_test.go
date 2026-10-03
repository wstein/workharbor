package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUsageValidate(t *testing.T) {
	good := Usage{
		Model: "claude-sonnet-5-5", Tokens: &TokenCounts{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 1},
		Cost:    &Cost{MicroUSD: 1200, Source: CostReported},
		Windows: []UsageWindow{{Name: WindowFiveHour, Utilization: 0.4, ResetsAt: time.Unix(1, 0)}, {Name: WindowSevenDay, Utilization: 1}},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a good payload: %v", err)
	}
	if err := (Usage{Model: "m"}).Validate(); err != nil {
		t.Errorf("a payload with no tokens, no cost and no windows: %v", err)
	}
	// Unknown is not zero: a missing count is nil, and the wire leaves it out.
	if raw, _ := json.Marshal(Usage{Model: "m"}); strings.Contains(string(raw), "tokens") {
		t.Errorf("unknown tokens are on the wire: %s", raw)
	}
	if raw, _ := json.Marshal(Usage{Model: "m", Tokens: &TokenCounts{}}); !strings.Contains(string(raw), `"tokens":{"input":0`) {
		t.Errorf("a reported zero must stay on the wire: %s", raw)
	}

	bad := map[string]func(*Usage){
		"no model":             func(u *Usage) { u.Model = "" },
		"negative input":       func(u *Usage) { u.Tokens = &TokenCounts{Input: -1} },
		"negative output":      func(u *Usage) { u.Tokens = &TokenCounts{Output: -1} },
		"negative cache read":  func(u *Usage) { u.Tokens = &TokenCounts{CacheRead: -1} },
		"negative cache write": func(u *Usage) { u.Tokens = &TokenCounts{CacheWrite: -1} },
		"negative cost":        func(u *Usage) { u.Cost = &Cost{MicroUSD: -1, Source: CostReported} },
		"unknown cost source":  func(u *Usage) { u.Cost = &Cost{MicroUSD: 1, Source: "guessed"} },
		"cost without source":  func(u *Usage) { u.Cost = &Cost{MicroUSD: 1} },
		"huge input":           func(u *Usage) { u.Tokens = &TokenCounts{Input: 9e18} },
		"huge cache write":     func(u *Usage) { u.Tokens = &TokenCounts{CacheWrite: MaxTokensPerTurn + 1} },
		"huge cost":            func(u *Usage) { u.Cost = &Cost{MicroUSD: MaxMicroUSDPerTurn + 1, Source: CostReported} },
		"huge duration":        func(u *Usage) { u.WallMillis = MaxMillisPerTurn + 1 },
		"unnamed window":       func(u *Usage) { u.Windows = []UsageWindow{{Utilization: 0.1}} },
		"utilization above 1":  func(u *Usage) { u.Windows = []UsageWindow{{Name: WindowFiveHour, Utilization: 1.1}} },
		"negative utilization": func(u *Usage) { u.Windows = []UsageWindow{{Name: WindowFiveHour, Utilization: -0.1}} },
	}
	for name, mod := range bad {
		u := Usage{Model: "m"}
		mod(&u)
		if err := u.Validate(); !errors.Is(err, ErrBadUsage) {
			t.Errorf("%s: Validate = %v, want ErrBadUsage", name, err)
		}
	}
}
