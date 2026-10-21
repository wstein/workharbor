//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package adapters

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

type fixtureExec struct {
	calls   []pkgmgr.Command
	block   bool
	process *fixtureProcess
}

func (f *fixtureExec) Start(_ context.Context, c pkgmgr.Command) (pkgmgr.Process, error) {
	f.calls = append(f.calls, c)
	p := &fixtureProcess{done: make(chan pkgmgr.Completion, 1)}
	f.process = p
	if !f.block {
		p.done <- pkgmgr.Completion{Exit: 0}
	}
	return p, nil
}

type fixtureProcess struct {
	done              chan pkgmgr.Completion
	interrupts, kills int
}

func (p *fixtureProcess) Done() <-chan pkgmgr.Completion { return p.done }
func (p *fixtureProcess) Interrupt() error               { p.interrupts++; return nil }
func (p *fixtureProcess) Kill() error                    { p.kills++; return nil }

type fixtureClock struct{}

func (fixtureClock) Now() time.Time                       { return time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC) }
func (fixtureClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

type fixtureBrewFS struct{}

func (fixtureBrewFS) BrewExecutable(p string) (bool, error) {
	return p == "/opt/homebrew/bin/brew", nil
}

type fixtureDetector struct{ calls map[string]int }

func (f *fixtureDetector) Detect(_ context.Context, i catalogue.Item) detect.Result {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[i.ID]++
	state := "absent"
	if f.calls[i.ID] > 1 {
		state = "present_ok"
	}
	return detect.Result{Item: i.ID, State: state, Managed: state == "present_ok"}
}

type fixtureBook struct{}

func (fixtureBook) Begin(context.Context, string) (pkgmgr.Journal, error) { return fixtureBook{}, nil }
func (fixtureBook) Record(context.Context, pkgmgr.Event) error            { return nil }
func (fixtureBook) Close(context.Context) error                           { return nil }
func brewFixture(t *testing.T) (pkgmgr.Engine, pkgmgr.Request, *fixtureExec, string) {
	t.Helper()
	home := t.TempDir()
	input := plan.Input{Platform: "darwin-arm64", Catalogue: &catalogue.Catalogue{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Items: []catalogue.Item{{ID: "jq", Kind: "tool", SelectedByDefault: true, Tool: catalogue.Tool{Command: "jq", MinVersion: "1.0.0", Source: catalogue.Source{Manager: "homebrew", Type: "formula", Name: "jq", Tap: "homebrew/core"}}}}}, Results: []detect.Result{{Item: "jq", State: "absent"}}}
	sealed, err := plan.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	exec := &fixtureExec{}
	engine := pkgmgr.Engine{Exec: exec, FS: fixtureBrewFS{}, Clock: fixtureClock{}, Detector: &fixtureDetector{}, Bookkeeper: fixtureBook{}}
	return engine, pkgmgr.Request{Input: input, ExpectedHash: sealed.Hash, Policy: pkgmgr.Policy{Mode: "setup", Yes: true, UID: 501}, Environment: map[string]string{"HOME": home}}, exec, home
}
func TestExistingBrewEnginePositiveControl(t *testing.T) {
	engine, request, exec, _ := brewFixture(t)
	out, err := engine.Apply(context.Background(), request)
	if err != nil || len(exec.calls) != 1 || len(out.Steps) != 1 || out.Steps[0].Status != "applied" {
		t.Fatal("actual reviewed T003 fixture failed", out, err, exec.calls)
	}
}
func TestConcreteBrewStepRunsExistingEngine(t *testing.T) {
	brew, request, exec, home := brewFixture(t)
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner, err := New(brew, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := plan.ApplyEngine{Store: store, Clock: fixtureClock{}, Runner: runner}
	out, err := engine.Apply(context.Background(), plan.ApplyRequest{Input: request.Input, ExpectedHash: request.ExpectedHash, RunID: "0123456789abcdef", Mode: "setup", Answer: "yes", Confirmed: true, UID: 501})
	if len(exec.calls) != 1 {
		t.Fatal("concrete adapter did not execute existing package-manager engine through fake Exec", len(exec.calls))
	}
	if err != nil || out.ExitCode != 0 || len(out.Steps) != 1 || out.Steps[0].Audit.Result != "applied" {
		t.Fatal(out, err)
	}
}

type deadlineClock struct{ budgets []time.Duration }

func (c *deadlineClock) After(budget time.Duration) <-chan time.Time {
	c.budgets = append(c.budgets, budget)
	ready := make(chan time.Time, 1)
	ready <- fixtureClock{}.Now()
	return ready
}
func TestConcreteBrewDeadlineKeepsExistingChildBounds(t *testing.T) {
	brew, request, exec, home := brewFixture(t)
	exec.block = true
	clock := &deadlineClock{}
	brew.Clock = clock
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Keep profile persistence a genuine no-op so it cannot mask 71 as partial73.
	seed, err := store.Begin(profile.Start{RunID: "1123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, fixtureClock{}.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = seed.StoreProfile(profile.HostProfile{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Updated: fixtureClock{}.Now().Format(time.RFC3339Nano), Items: map[string]string{"jq": "selected"}}); err != nil {
		t.Fatal(err)
	}
	seed.Close()
	runner, err := New(brew, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := (plan.ApplyEngine{Store: store, Clock: fixtureClock{}, Runner: runner}).Apply(context.Background(), plan.ApplyRequest{Input: request.Input, ExpectedHash: request.ExpectedHash, RunID: "0123456789abcdef", Mode: "setup", Answer: "yes", Confirmed: true, UID: 501})
	if err != nil || out.ExitCode != 71 || out.ProfileChanged || out.Steps[0].Reason != "step_timeout" {
		t.Fatal("actual T003 deadline was lost", out, err)
	}
	if exec.process == nil || exec.process.interrupts != 1 || exec.process.kills != 1 || !reflect.DeepEqual(clock.budgets, []time.Duration{900 * time.Second, 30 * time.Second}) {
		t.Fatal("existing T003 bounds/cleanup changed", exec.process, clock.budgets)
	}
}

type fixtureGuard struct {
	calls     int
	err       error
	completed map[string]bool
}

func (g *fixtureGuard) Checkpoint() error        { g.calls++; return g.err }
func (g *fixtureGuard) Completed(id string) bool { return g.completed[id] }
func TestConcreteBrewChecksRealStartingGuardBeforeExec(t *testing.T) {
	brew, request, exec, _ := brewFixture(t)
	runner, err := New(brew, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := plan.Build(request.Input)
	if err != nil {
		t.Fatal(err)
	}
	guard := &fixtureGuard{err: errors.New("PRIVATE-GUARD-CANARY")}
	out, err := runner.ApplyStep(context.Background(), shown.Steps[0], "0123456789abcdef", plan.StepBudget, guard)
	if err != nil || out.Audit.Result != "blocked" || guard.calls != 1 || len(exec.calls) != 0 || out.Reason != "protocol_unwritable" {
		t.Fatal("actual T003 starting callback bypassed guard", out, err, guard.calls, exec.calls)
	}
}
func TestConcreteBrewRequiresOwnedDependencyEvidence(t *testing.T) {
	brew, request, exec, _ := brewFixture(t)
	request.Input.Catalogue.Items[0].Requires = []string{"bat"}
	request.Input.Catalogue.Items = append(request.Input.Catalogue.Items, catalogue.Item{ID: "bat", Kind: "tool", SelectedByDefault: true})
	request.Input.Results = append(request.Input.Results, detect.Result{Item: "bat", State: "absent"})
	shown, err := plan.Build(request.Input)
	if err != nil {
		t.Fatal(err)
	}
	request.ExpectedHash = shown.Hash
	// A current version probe alone does not prove the earlier planned action.
	brew.Detector.(*fixtureDetector).calls = map[string]int{"bat": 1}
	guard := &fixtureGuard{}
	out, err := (pkgmgr.StepAdapter{Engine: brew, Request: request}).ApplyStep(context.Background(), shown.Steps[1], "0123456789abcdef", plan.StepBudget, guard)
	if err != nil || out.Audit.Result != "blocked" || len(exec.calls) != 0 || guard.calls != 0 {
		t.Fatal("missing owned dependency evidence was invented", out, err, exec.calls)
	}
}
func TestConcreteBrewRefusesUnconfirmedOrStaleAuthority(t *testing.T) {
	for _, kind := range []string{"root", "sudo-user", "dry-run", "doctor", "unconfirmed", "stale", "step-tamper"} {
		t.Run(kind, func(t *testing.T) {
			brew, request, exec, _ := brewFixture(t)
			shown, err := plan.Build(request.Input)
			if err != nil {
				t.Fatal(err)
			}
			step := shown.Steps[0]
			switch kind {
			case "root":
				request.Policy.UID = 0
			case "sudo-user":
				request.Policy.SUDOUserSet = true
			case "dry-run":
				request.Policy.DryRun = true
			case "doctor":
				request.Policy.Mode = "doctor"
			case "unconfirmed":
				request.Policy.Yes = false
			case "stale":
				request.ExpectedHash = strings.Repeat("a", 64)
			case "step-tamper":
				step.Item = "bat"
			}
			_, err = (pkgmgr.StepAdapter{Engine: brew, Request: request}).ApplyStep(context.Background(), step, "0123456789abcdef", plan.StepBudget, &fixtureGuard{})
			if err == nil || len(exec.calls) != 0 {
				t.Fatal("unconfirmed/stale authority executed actual package engine", kind, err, exec.calls)
			}
		})
	}
}

type stuckProcess struct {
	fixtureProcess
	killErr error
}

func (p *stuckProcess) Kill() error { p.kills++; return p.killErr }

type stuckExec struct {
	process  *stuckProcess
	startErr func(*stuckProcess) error
	onStart  func()
}

func (x *stuckExec) Start(_ context.Context, _ pkgmgr.Command) (pkgmgr.Process, error) {
	if x.onStart != nil {
		x.onStart()
	}
	p := x.process
	p.done = make(chan pkgmgr.Completion, 1)
	if x.startErr != nil {
		return p, x.startErr(p)
	}
	return p, nil
}

// A cleanup failure keeps ownership of the started child. It must reach the
// orchestration caller unchanged and win over the deadline/cancel mapping.
func TestConcreteBrewCleanupErrorReachesCallerAndWinsOverTimeoutMapping(t *testing.T) {
	cause := errors.New("kill refused")
	for _, mode := range []string{"startup", "deadline", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			brew, request, _, _ := brewFixture(t)
			proc := &stuckProcess{killErr: cause}
			exec := &stuckExec{process: proc}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "startup":
				exec.startErr = func(p *stuckProcess) error { return pkgmgr.NewCleanupError(p, cause) }
			case "deadline":
				brew.Clock = &deadlineClock{}
			case "cancel":
				brew.Clock = &deadlineClock{}
				exec.onStart = cancel
			}
			brew.Exec = exec
			shown, err := plan.Build(request.Input)
			if err != nil {
				t.Fatal(err)
			}
			out, err := (pkgmgr.StepAdapter{Engine: brew, Request: request}).ApplyStep(ctx, shown.Steps[0], "0123456789abcdef", plan.StepBudget, &fixtureGuard{})
			var owned pkgmgr.CleanupError
			if !errors.As(err, &owned) || owned.CleanupProcess() != pkgmgr.Process(proc) || !errors.Is(err, cause) {
				t.Fatal("cleanup ownership was lost or replaced", out, err)
			}
			if errors.Is(err, context.DeadlineExceeded) || (mode == "cancel" && err == ctx.Err()) {
				t.Fatal("timeout/cancel mapping replaced the cleanup error", err)
			}
		})
	}
}

// Through the real orchestration path a retained child handle stops the run
// before the next independent step and reaches the caller as the same error.
func TestConcreteBrewCleanupErrorStopsRunAndKeepsHandle(t *testing.T) {
	brew, request, _, home := brewFixture(t)
	request.Input.Catalogue.Items = append(request.Input.Catalogue.Items, catalogue.Item{ID: "bat", Kind: "tool", SelectedByDefault: true, Tool: catalogue.Tool{Command: "bat", MinVersion: "1.0.0", Source: catalogue.Source{Manager: "homebrew", Type: "formula", Name: "bat", Tap: "homebrew/core"}}})
	request.Input.Results = append(request.Input.Results, detect.Result{Item: "bat", State: "absent"})
	sealed, err := plan.Build(request.Input)
	if err != nil || len(sealed.Steps) != 2 {
		t.Fatal(sealed, err)
	}
	request.ExpectedHash = sealed.Hash
	cause := errors.New("kill refused")
	proc := &stuckProcess{killErr: cause}
	starts := 0
	exec := &stuckExec{process: proc, onStart: func() { starts++ }}
	exec.startErr = func(p *stuckProcess) error { return pkgmgr.NewCleanupError(p, cause) }
	brew.Exec = exec
	store, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner, err := New(brew, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := (plan.ApplyEngine{Store: store, Clock: fixtureClock{}, Runner: runner}).Apply(context.Background(), plan.ApplyRequest{Input: request.Input, ExpectedHash: request.ExpectedHash, RunID: "0123456789abcdef", Mode: "setup", Answer: "yes", Confirmed: true, UID: 501})
	var owned pkgmgr.CleanupError
	if !errors.As(err, &owned) || owned.CleanupProcess() != pkgmgr.Process(proc) || !errors.Is(err, cause) {
		t.Fatal("retained handle did not reach the ApplyEngine caller", out, err)
	}
	if starts != 1 || len(out.Steps) != 1 || out.Steps[0].Reason != "package_manager_failed" || len(out.Remaining) != 2 || out.ProfileChanged {
		t.Fatal("run continued after a retained child handle", starts, out)
	}
}
