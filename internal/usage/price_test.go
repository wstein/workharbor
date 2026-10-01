package usage

import (
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

var table = PriceTable{Version: "test-1", Models: map[string]Price{
	"m": {Input: 3_000_000, Output: 15_000_000, CacheRead: 300_000, CacheWrite: 3_750_000},
}}

func TestEstimateIsLabelledAndRounded(t *testing.T) {
	// 1000 in, 500 out, 10000 cache read, 200 cache write.
	c, ok := table.Estimate("m", domain.UsageTokens{Input: 1000, Output: 500, CacheRead: 10000, CacheWrite: 200})
	// 3000 + 7500 + 3000 + 750 = 14250 micro-USD
	if !ok || c.MicroUSD != 14250 || c.Source != domain.CostEstimated || c.PriceTable != "test-1" {
		t.Errorf("Estimate = %+v, %v", c, ok)
	}
	// Half a micro-dollar rounds up, never down to a free turn.
	c, _ = table.Estimate("m", domain.UsageTokens{CacheRead: 2}) // 0.6 micro
	if c.MicroUSD != 1 {
		t.Errorf("0.6 micro-USD = %d", c.MicroUSD)
	}
}

func TestAnUnknownModelIsNotPricedAtZero(t *testing.T) {
	if _, ok := table.Estimate("other", domain.UsageTokens{Input: 1}); ok {
		t.Error("a model without a price must not be estimated")
	}
	if _, ok := (PriceTable{}).Estimate("m", domain.UsageTokens{}); ok {
		t.Error("an empty table estimates nothing")
	}
}

func TestPriceTableValidate(t *testing.T) {
	if err := table.Validate(); err != nil {
		t.Error(err)
	}
	if (PriceTable{}).Validate() == nil {
		t.Error("a table needs a version")
	}
	bad := PriceTable{Version: "v", Models: map[string]Price{"m": {Input: -1}}}
	if bad.Validate() == nil {
		t.Error("a negative price is refused")
	}
}
