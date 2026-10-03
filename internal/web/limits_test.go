package web

import (
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

// Nothing reported shows unknown with its source, never zero, full or an estimate.
func TestTheLimitsPanelSaysUnknownWhenNothingWasReported(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	_, page := b.do("GET", "/", nil)
	for _, want := range []string{"Limits", "claude", "unknown", "not reported by the claude adapter"} {
		if !strings.Contains(page, want) {
			t.Errorf("the limits panel lacks %q", want)
		}
	}
	for _, bad := range []string{`id="lim-claude`, "0% used", "100% left", "Low limit"} {
		if strings.Contains(page, bad) {
			t.Errorf("the limits panel shows %q for an unknown limit", bad)
		}
	}
}

// A reading shows used and left, the reset, the source and its age; a low one warns;
// an API key shows its configured budget.
func TestTheLimitsPanelShowsAReadingItsAgeAndALowWarning(t *testing.T) {
	r := newRig(t)
	r.be.limits = []service.ProviderLimits{{
		Provider: "claude", Known: true, Source: "reported by the claude adapter", Low: true,
		Windows:        []service.LimitWindow{{Name: "five_hour", Used: 0.93, ResetsAt: t0.Add(2 * time.Hour), At: t0.Add(-10 * time.Minute), Low: true}},
		Balance:        &store.BalanceReading{Agent: "claude", RemainingMicroUSD: 12_340_000, At: t0.Add(-5 * time.Minute)},
		APIKey:         true,
		BudgetMicroUSD: 5_000_000,
	}}
	b := r.browser()
	b.signIn()
	_, page := b.do("GET", "/", nil)
	for _, want := range []string{
		`id="lim-claude-five-hour"`, "93% used, 7% left", "resets in 2h 0m", "read 10m ago",
		"Low limit", "$12.3400 left", "Source: reported by the claude adapter", "$5.0000 per task (API key)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the limits panel lacks %q", want)
		}
	}
}
