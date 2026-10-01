// Package usage estimates what a turn cost when the agent reports tokens but
// no cost (design §5.7). An estimate comes from a pinned, dated price table
// that the supervisor's configuration provides, and is always labelled as an
// estimate. The package ships no prices: a figure workharbor could not source
// would look like a fact.
package usage

import (
	"errors"
	"fmt"

	"github.com/wstein/workharbor/internal/domain"
)

// Price is what a model costs per million tokens, in millionths of a US dollar.
type Price struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// PriceTable is a set of prices as of a date. Version names it in every
// estimate made from it, so a changed table is told apart from the old one.
type PriceTable struct {
	Version string           `json:"version"` // for example "2026-10-01"
	Source  string           `json:"source"`  // where the prices were read, for the human
	Models  map[string]Price `json:"models"`  // by the model ID the agent reports
}

// Validate checks that a table can be used.
func (t PriceTable) Validate() error {
	if t.Version == "" {
		return errors.New("usage: a price table needs a version")
	}
	for m, p := range t.Models {
		if m == "" || p.Input < 0 || p.Output < 0 || p.CacheRead < 0 || p.CacheWrite < 0 {
			return fmt.Errorf("usage: price table %s: model %q has a negative price", t.Version, m)
		}
	}
	return nil
}

// Estimate prices a turn's tokens. ok is false when the table has no price for
// the model: an unknown model is not priced at zero.
func (t PriceTable) Estimate(model string, tok domain.UsageTokens) (cost domain.UsageCost, ok bool) {
	p, found := t.Models[model]
	if !found || t.Version == "" {
		return domain.UsageCost{}, false
	}
	const perMillion = 1_000_000
	sum := tok.Input*p.Input + tok.Output*p.Output + tok.CacheRead*p.CacheRead + tok.CacheWrite*p.CacheWrite
	return domain.UsageCost{MicroUSD: (sum + perMillion/2) / perMillion, Source: domain.CostEstimated, PriceTable: t.Version}, true
}
