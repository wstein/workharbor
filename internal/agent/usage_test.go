package agent

import (
	"errors"
	"testing"
	"time"
)

func TestUsageValidate(t *testing.T) {
	good := Usage{
		Model: "claude-sonnet-5-5", InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 1,
		Cost:    &Cost{MicroUSD: 1200, Source: CostReported},
		Windows: []UsageWindow{{Name: WindowFiveHour, Utilization: 0.4, ResetsAt: time.Unix(1, 0)}, {Name: WindowSevenDay, Utilization: 1}},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a good payload: %v", err)
	}
	if err := (Usage{Model: "m"}).Validate(); err != nil {
		t.Errorf("tokens only, no cost and no windows: %v", err)
	}

	bad := map[string]func(*Usage){
		"no model":             func(u *Usage) { u.Model = "" },
		"negative input":       func(u *Usage) { u.InputTokens = -1 },
		"negative output":      func(u *Usage) { u.OutputTokens = -1 },
		"negative cache read":  func(u *Usage) { u.CacheReadTokens = -1 },
		"negative cache write": func(u *Usage) { u.CacheWriteTokens = -1 },
		"negative cost":        func(u *Usage) { u.Cost = &Cost{MicroUSD: -1, Source: CostReported} },
		"unknown cost source":  func(u *Usage) { u.Cost = &Cost{MicroUSD: 1, Source: "guessed"} },
		"cost without source":  func(u *Usage) { u.Cost = &Cost{MicroUSD: 1} },
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
