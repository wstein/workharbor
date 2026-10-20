// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package profile_test

import (
	"context"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

// These ports execute the real T003 engine without permitting child execution,
// host filesystem access, timers, or network. The journal captures its actual
// finished event rather than fabricating an empty-argv success record.
type noChildPorts struct {
	t     *testing.T
	state string
}

func (p noChildPorts) Start(context.Context, pkgmgr.Command) (pkgmgr.Process, error) {
	p.t.Fatal("no-child outcome started a process")
	return nil, nil
}
func (p noChildPorts) After(time.Duration) <-chan time.Time {
	p.t.Fatal("no-child outcome started a timer")
	return nil
}
func (p noChildPorts) BrewExecutable(string) (bool, error) { return false, nil }
func (p noChildPorts) Detect(_ context.Context, i catalogue.Item) detect.Result {
	return detect.Result{Item: i.ID, State: p.state, Managed: p.state == "present_outdated"}
}

type captureNoChild struct {
	events []pkgmgr.Event
	cancel context.CancelFunc
}

func (j *captureNoChild) Begin(context.Context, string) (pkgmgr.Journal, error) {
	if j.cancel != nil {
		j.cancel()
	}
	return j, nil
}
func (j *captureNoChild) Record(_ context.Context, e pkgmgr.Event) error {
	j.events = append(j.events, e)
	return nil
}
func (j *captureNoChild) Close(context.Context) error { return nil }

func TestProtocolAcceptsActualPackageManagerNoChildEvents(t *testing.T) {
	for _, kind := range []string{"brew_install", "brew_upgrade"} {
		outcomes := []string{"blocked", "skipped", "unchanged"}
		if kind == "brew_upgrade" {
			outcomes = append(outcomes, "declined")
		}
		for _, outcome := range outcomes {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				observed := "absent"
				if kind == "brew_upgrade" {
					observed = "present_outdated"
				}
				input := plan.Input{Platform: "darwin-arm64", Catalogue: &catalogue.Catalogue{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Items: []catalogue.Item{{ID: "jq", Kind: "tool", SelectedByDefault: true, Tool: catalogue.Tool{Command: "jq", Source: catalogue.Source{Manager: "homebrew", Type: "formula", Name: "jq", Tap: "homebrew/core"}}}}}, Results: []detect.Result{{Item: "jq", State: observed, Managed: kind == "brew_upgrade"}}}
				sealed, err := plan.Build(input)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				capture := &captureNoChild{}
				ports := noChildPorts{t: t, state: observed}
				if outcome == "unchanged" {
					ports.state = "present_ok"
				}
				if outcome == "skipped" {
					capture.cancel = cancel
				}
				engine := pkgmgr.Engine{Exec: ports, FS: ports, Clock: ports, Detector: ports, Bookkeeper: capture}
				request := pkgmgr.Request{Input: input, ExpectedHash: sealed.Hash, Policy: pkgmgr.Policy{Mode: "setup", Yes: true, UID: 501, Confirmed: map[string]bool{"jq": outcome != "declined"}}}
				if _, err = engine.Apply(ctx, request); err != nil {
					t.Fatal(err)
				}
				if len(capture.events) != 1 {
					t.Fatalf("expected one finished no-child event, got %+v", capture.events)
				}
				event := capture.events[0]
				if event.Stage != "finished" || event.Status != outcome || event.Kind != kind || event.Argv != nil || event.Exit != nil {
					t.Fatalf("unexpected T003 no-child event: %+v", event)
				}
				r := begin(t, openStore(t, t.TempDir()), "0123456789abcdef")
				if err = r.Step(profile.StepAudit{ID: event.StepID, Kind: event.Kind, Item: event.Item, Result: event.Status, Argv: event.Argv, ExitStatus: event.Exit}, instant); err != nil {
					t.Fatalf("actual T003 %s no-child event rejected: %v", outcome, err)
				}
			})
		}
	}
}
func TestProtocolRejectsBrewEmptyArgvForChildOutcomes(t *testing.T) {
	for _, kind := range []string{"brew_install", "brew_upgrade"} {
		t.Run(kind, func(t *testing.T) {
			r := begin(t, openStore(t, t.TempDir()), "0123456789abcdef")
			for _, status := range []string{"applied", "failed", "rolled_back", "opted_out"} {
				if err := r.Step(profile.StepAudit{ID: "jq." + kind, Kind: kind, Item: "jq", Result: status}, instant); err == nil {
					t.Fatalf("empty argv admitted for %s", status)
				}
			}
			// An outcome that permits no child still cannot admit arbitrary
			// argv: executed command validation retains the catalogue boundary.
			for _, status := range []string{"blocked", "declined", "skipped", "unchanged"} {
				a := profile.StepAudit{ID: "jq." + kind, Kind: kind, Item: "jq", Result: status, Argv: []string{"/opt/homebrew/bin/brew", "install", "TOKEN-CANARY"}}
				if err := r.Step(a, instant); err == nil {
					t.Fatalf("arbitrary argv admitted for %s", status)
				}
			}
			zero := 0
			for _, a := range []profile.StepAudit{
				{ID: "jq." + kind, Kind: kind, Item: "jq", Result: "unchanged", ExitStatus: &zero},
				{ID: "jq." + kind, Kind: kind, Item: "jq", Result: "blocked", Sudo: true},
				{ID: "jq." + kind, Kind: kind, Item: "jq", Result: "skipped", Network: true},
			} {
				if err := r.Step(a, instant); err == nil {
					t.Fatalf("no-child audit admitted child effects: %+v", a)
				}
			}
		})
	}
}
