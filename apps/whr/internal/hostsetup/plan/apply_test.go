//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

type applyClock struct{ now time.Time }

func (c *applyClock) Now() time.Time { return c.now }

type applyRunner struct {
	calls    []Step
	callback func(context.Context, Step, string, time.Duration) (StepOutcome, error)
}

func (r *applyRunner) ApplyStep(ctx context.Context, s Step, runID string, budget time.Duration) (StepOutcome, error) {
	r.calls = append(r.calls, s)
	if r.callback != nil {
		return r.callback(ctx, s, runID, budget)
	}
	return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}, Changed: true}, nil
}
func applyFixture(t *testing.T) (ApplyEngine, ApplyRequest, *applyRunner, string) {
	t.Helper()
	home := t.TempDir()
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	in := input()
	in.Platform = "darwin-arm64"
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	runner := &applyRunner{}
	return ApplyEngine{Store: store, Clock: &applyClock{now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}, Runner: runner}, ApplyRequest{Input: in, ExpectedHash: p.Hash, RunID: "0123456789abcdef", Mode: "setup", Answer: "yes", Confirmed: true, UID: 501}, runner, home
}
func TestApplyProtocolUnwritableBlocksAllSteps(t *testing.T) {
	e, r, runner, home := applyFixture(t)
	dir := filepath.Join(home, ".local/state/whr/protocols")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, r.RunID+".jsonl")
	if err := os.WriteFile(path, []byte("retained protocol"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := e.Apply(context.Background(), r)
	if len(runner.calls) != 0 {
		t.Fatal("runner mutated before a writable durable protocol")
	}
	if err == nil {
		t.Fatal("unwritable protocol was accepted")
	}
}
func resealApply(t *testing.T, r *ApplyRequest) {
	t.Helper()
	p, err := Build(r.Input)
	if err != nil {
		t.Fatal(err)
	}
	r.ExpectedHash = p.Hash
}
func readApply(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func addBat(r *ApplyRequest) { r.Input.Selected["bat"] = true }
func TestApplyRerunNoOpStillJournalsAndLocks(t *testing.T) {
	e, r, runner, home := applyFixture(t)
	r.Input.Results[0].State = "present_ok"
	resealApply(t, &r)
	first, err := e.Apply(context.Background(), r)
	if err != nil || !first.ProfileChanged || len(runner.calls) != 0 {
		t.Fatal(first, err)
	}
	path := filepath.Join(home, ".config/whr/host-profile.json")
	before := readApply(t, path)
	info, _ := os.Stat(path)
	r.RunID = "1123456789abcdef"
	second, err := e.Apply(context.Background(), r)
	if err != nil || second.ProfileChanged || len(second.Steps) != 0 || len(runner.calls) != 0 || second.ExitCode != 0 {
		t.Fatal(second, err)
	}
	after, _ := os.Stat(path)
	if string(before) != string(readApply(t, path)) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("rerun rewrote managed profile")
	}
	data := readApply(t, filepath.Join(home, ".local/state/whr/protocols/"+r.RunID+".jsonl"))
	if strings.Count(string(data), "\n") != 3 {
		t.Fatal("no-op protocol lacks exactly start/selection/end", string(data))
	}
	if _, err := os.Stat(filepath.Join(home, ".local/state/whr/backups")); !os.IsNotExist(err) {
		t.Fatal("no-op created backups")
	}
	r.RunID = "2123456789abcdef"
	r.Input.Results[0].State = "absent"
	resealApply(t, &r)
	runner.callback = func(ctx context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
		_, err := e.Store.Begin(profile.Start{RunID: "3123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, e.Clock.Now())
		if !errors.Is(err, profile.ErrLocked) {
			t.Fatal("apply runner did not hold exclusive lock", err)
		}
		return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}}, nil
	}
	if _, err = e.Apply(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	next, err := e.Store.Begin(profile.Start{RunID: "3123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, e.Clock.Now())
	if err != nil {
		t.Fatal("apply retained lock", err)
	}
	next.Close()
}
func TestApplyFailureBlocksOnlyDependents(t *testing.T) {
	e, r, runner, _ := applyFixture(t)
	addBat(&r)
	r.Input.Catalogue.Items[0].Requires = []string{"bat"}
	r.Input.Catalogue.Items = append(r.Input.Catalogue.Items, catalogue.Item{ID: "fd", SelectedByDefault: true})
	r.Input.Results = append(r.Input.Results, detect.Result{Item: "fd", State: "absent"})
	resealApply(t, &r)
	runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
		status := "applied"
		exit := 0
		if s.Item == "bat" {
			status = "failed"
			exit = 1
		}
		return StepOutcome{Audit: profile.StepAudit{Result: status, Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}, ExitStatus: &exit}}, nil
	}
	out, err := e.Apply(context.Background(), r)
	if err != nil || out.ExitCode != 73 {
		t.Fatal(out, err)
	}
	if len(runner.calls) != 2 || runner.calls[0].Item != "bat" || runner.calls[1].Item != "fd" {
		t.Fatal("failure escaped dependent subgraph", runner.calls)
	}
	if !reflect.DeepEqual(out.Remaining, []string{"bat.brew_install", "jq.brew_install"}) {
		t.Fatal(out.Remaining)
	}
	if out.Steps[1].Audit.Result != "blocked" || out.Steps[1].Reason != "dependency_failed" {
		t.Fatal(out.Steps[1])
	}
}
func TestApplyInterruptStopsBeforeNextStepAndProfile(t *testing.T) {
	e, r, runner, home := applyFixture(t)
	addBat(&r)
	resealApply(t, &r)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
		cancel()
		return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}, Changed: true}, nil
	}
	out, err := e.Apply(ctx, r)
	if err != nil || out.ExitCode != 73 || len(runner.calls) != 1 {
		t.Fatal(out, err, runner.calls)
	}
	if !reflect.DeepEqual(out.Applied, []string{"jq.brew_install"}) || !reflect.DeepEqual(out.Remaining, []string{"bat.brew_install"}) {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("interruption performed later profile operation")
	}
	data := readApply(t, filepath.Join(home, ".local/state/whr/protocols/"+r.RunID+".jsonl"))
	if !strings.Contains(string(data), `"result":"skipped"`) {
		t.Fatal("interruption did not record remaining step")
	}
}
func TestApplyReadOnlyAndNoWriteNothing(t *testing.T) {
	for _, mode := range []string{"dry-run", "doctor", "no", "unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			e, r, runner, home := applyFixture(t)
			switch mode {
			case "dry-run":
				r.DryRun = true
			case "doctor":
				r.Mode = "doctor"
			case "no":
				r.Answer = "no"
			case "unconfirmed":
				r.Confirmed = false
			}
			out, err := e.Apply(context.Background(), r)
			if mode == "unconfirmed" {
				if err == nil || out.ExitCode != 70 {
					t.Fatal(out, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 || len(runner.calls) != 0 {
				t.Fatal("read-only request mutated", entries, runner.calls, err)
			}
		})
	}
}
func TestApplyRechecksHashAndConsent(t *testing.T) {
	e, r, runner, home := applyFixture(t)
	r.ExpectedHash = strings.Repeat("a", 64)
	if _, err := e.Apply(context.Background(), r); err == nil || len(runner.calls) != 0 {
		t.Fatal("stale confirmation accepted")
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatal("stale plan created bookkeeping")
	}
	r.Input.Results[0] = detect.Result{Item: "jq", State: "present_outdated", Managed: true}
	resealApply(t, &r)
	out, err := e.Apply(context.Background(), r)
	if err != nil || out.ExitCode != 0 || len(runner.calls) != 0 || out.Steps[0].Audit.Result != "declined" {
		t.Fatal("run confirmation approved modify step", out, err)
	}
}
func TestApplyPerStepDeadlineIsNotGlobalRunBudget(t *testing.T) {
	e, r, runner, _ := applyFixture(t)
	addBat(&r)
	resealApply(t, &r)
	clock := e.Clock.(*applyClock)
	runner.callback = func(ctx context.Context, s Step, runID string, budget time.Duration) (StepOutcome, error) {
		if runID != r.RunID {
			t.Fatal("runner backup namespace differs from durable run")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > StepBudget || budget != 900*time.Second {
			t.Fatal("step deadline/budget missing", deadline, budget)
		}
		clock.now = clock.now.Add(600 * time.Second)
		return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}}, nil
	}
	out, err := e.Apply(context.Background(), r)
	if err != nil || out.ExitCode != 0 || len(runner.calls) != 2 {
		t.Fatal("900s incorrectly bounded entire run", out, err)
	}
}
func TestApplyStepTimeoutAndParentDeadline(t *testing.T) {
	t.Run("step", func(t *testing.T) {
		e, r, runner, _ := applyFixture(t)
		addBat(&r)
		resealApply(t, &r)
		clock := e.Clock.(*applyClock)
		runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
			if s.Item == "jq" {
				clock.now = clock.now.Add(StepBudget)
			}
			return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}, Changed: true}, nil
		}
		out, err := e.Apply(context.Background(), r)
		if err != nil || out.ExitCode != 73 || len(runner.calls) != 2 || !reflect.DeepEqual(out.Remaining, []string{"jq.brew_install"}) || out.Steps[0].Reason != "step_timeout" {
			t.Fatal(out, err)
		}
	})
	t.Run("parent", func(t *testing.T) {
		e, r, runner, home := applyFixture(t)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		out, err := e.Apply(ctx, r)
		entries, _ := os.ReadDir(home)
		if err == nil || out.ExitCode != 71 || len(runner.calls) != 0 || len(entries) != 0 {
			t.Fatal(out, err)
		}
	})
}
func TestApplyRejectsSensitiveRunnerMetadataAndStops(t *testing.T) {
	e, r, runner, home := applyFixture(t)
	addBat(&r)
	resealApply(t, &r)
	runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
		return StepOutcome{Audit: profile.StepAudit{Result: "failed", Argv: []string{"/opt/homebrew/bin/brew", "install", "TOKEN-CANARY"}}, Changed: true, Reason: "ENV-CANARY"}, errors.New("PASSWORD-CANARY")
	}
	out, err := e.Apply(context.Background(), r)
	if err == nil || out.ExitCode != 73 || len(runner.calls) != 1 {
		t.Fatal(out, err)
	}
	raw, _ := json.Marshal(out)
	data := readApply(t, filepath.Join(home, ".local/state/whr/protocols/"+r.RunID+".jsonl"))
	for _, secret := range []string{"TOKEN-CANARY", "ENV-CANARY", "PASSWORD-CANARY"} {
		if strings.Contains(string(raw)+string(data)+err.Error(), secret) {
			t.Fatal("runner metadata leaked")
		}
	}
}
func TestApplyFreezesSelectionsAndModifyConfirmations(t *testing.T) {
	e, r, runner, _ := applyFixture(t)
	addBat(&r)
	r.Input.Results[1] = detect.Result{Item: "bat", State: "present_outdated", Managed: true}
	r.ConfirmedItems = map[string]bool{"bat": false}
	resealApply(t, &r)
	runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
		r.ConfirmedItems["bat"] = true
		r.Input.Selected["bat"] = false
		return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}}, nil
	}
	out, err := e.Apply(context.Background(), r)
	if err != nil || len(runner.calls) != 1 || out.Steps[1].Audit.Result != "declined" {
		t.Fatal(out, err)
	}
	p, err := e.Store.ReadProfile()
	if err != nil || p.Items["bat"] != "selected" {
		t.Fatal("callback changed remembered consent", p, err)
	}
}
func TestApplyDispatchesAllKindsAndIsolatesConfigFailure(t *testing.T) {
	e, r, runner, home := applyFixture(t)
	addBat(&r)
	r.Input.Catalogue.Items[0].Config = &catalogue.Config{Target: ".config/jq/config"}
	r.Input.Config["jq"] = ConfigObservation{State: "absent"}
	resealApply(t, &r)
	runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
		if s.Kind == "dotfile_create" {
			return StepOutcome{Audit: profile.StepAudit{Result: "failed"}, Backups: []BackupAudit{{Path: "~/.local/state/whr/backups/" + r.RunID + "/jq.bak", SHA256: strings.Repeat("a", 64)}}, Diff: &DiffAudit{Added: 2, Removed: 0}}, nil
		}
		return StepOutcome{Audit: profile.StepAudit{Result: "applied", Argv: []string{"/opt/homebrew/bin/brew", "install", s.Item}}}, nil
	}
	out, err := e.Apply(context.Background(), r)
	if err != nil || out.ExitCode != 73 || len(runner.calls) != 3 || runner.calls[1].Kind != "dotfile_create" || runner.calls[2].Item != "bat" || !reflect.DeepEqual(out.Remaining, []string{"jq.dotfile_create"}) {
		t.Fatal(out, err, runner.calls)
	}
	raw := string(readApply(t, filepath.Join(home, ".local/state/whr/protocols/"+r.RunID+".jsonl")))
	for _, kind := range []string{`"type":"backup"`, `"type":"diff"`, `"kind":"dotfile_create"`} {
		if !strings.Contains(raw, kind) {
			t.Fatal("missing typed runner receipt", kind)
		}
	}
	// Receipts are injected audit evidence here, not a claim that the absent T004
	// adapter has created native files/backups in this source increment.
}
func TestApplyDeclineBlocksOnlySelectedDependents(t *testing.T) {
	e, r, runner, _ := applyFixture(t)
	addBat(&r)
	r.Input.Results[0] = detect.Result{Item: "jq", State: "present_outdated", Managed: true}
	r.Input.Catalogue.Items[0].Config = &catalogue.Config{Target: ".config/jq/config"}
	r.Input.Config["jq"] = ConfigObservation{State: "absent"}
	resealApply(t, &r)
	out, err := e.Apply(context.Background(), r)
	if err != nil || out.ExitCode != 73 || len(runner.calls) != 1 || runner.calls[0].Item != "bat" || out.Steps[0].Audit.Result != "declined" || out.Steps[1].Audit.Result != "blocked" {
		t.Fatal(out, err)
	}
}
func TestApplyOptOutPersistsIntoNextPlan(t *testing.T) {
	e, r, runner, _ := applyFixture(t)
	r.Input.OptedOut["jq"] = true
	r.Input.OptedOut["git-config"] = true
	resealApply(t, &r)
	out, err := e.Apply(context.Background(), r)
	if err != nil || len(runner.calls) != 0 || !out.ProfileChanged {
		t.Fatal(out, err)
	}
	remembered, err := e.Store.ReadProfile()
	if err != nil {
		t.Fatal(err)
	}
	r.Input.Selected, r.Input.OptedOut = remembered.Selection()
	resealApply(t, &r)
	next, err := Build(r.Input)
	if err != nil || len(next.Steps) != 0 || !r.Input.OptedOut["git-config"] {
		t.Fatal("rerun forgot opt-outs", next, err)
	}
}
func TestApplyCancelledRunnerBeforeChildIsSkipped(t *testing.T) {
	e, r, runner, _ := applyFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.callback = func(context.Context, Step, string, time.Duration) (StepOutcome, error) {
		cancel()
		return StepOutcome{}, context.Canceled
	}
	out, err := e.Apply(ctx, r)
	if err != nil || out.ExitCode != 72 || out.Steps[0].Audit.Result != "skipped" || out.ProfileChanged {
		t.Fatal(out, err)
	}
}
