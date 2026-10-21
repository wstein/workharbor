// SPDX-License-Identifier: EUPL-1.2
package pkgmgr

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
)

type fakeProcess struct {
	done              chan Completion
	interrupts, kills int
	finishOnInterrupt bool
}

func (p *fakeProcess) Done() <-chan Completion { return p.done }
func (p *fakeProcess) Interrupt() error {
	p.interrupts++
	if p.finishOnInterrupt {
		p.done <- Completion{Exit: 130}
	}
	return nil
}
func (p *fakeProcess) Kill() error { p.kills++; return nil }

type fakeExec struct {
	onStart func(context.Context)

	t         *testing.T
	allowed   [][]string
	calls     []Command
	processes []*fakeProcess
	exit      int
	err       error
}

func (f *fakeExec) Start(ctx context.Context, c Command) (Process, error) {
	f.t.Helper()
	if f.onStart != nil {
		f.onStart(ctx)
	}
	allowed := false
	for _, argv := range f.allowed {
		if reflect.DeepEqual(c.Argv, argv) {
			allowed = true
		}
	}
	if !allowed {
		f.t.Fatalf("unexpected argv: %q", c.Argv)
	}
	f.calls = append(f.calls, c)
	if f.err != nil {
		return nil, f.err
	}
	p := &fakeProcess{done: make(chan Completion, 1)}
	if len(f.processes) > 0 {
		p = f.processes[0]
		f.processes = f.processes[1:]
	} else {
		p.done <- Completion{Exit: f.exit}
	}
	return p, nil
}

type fakeFS struct {
	paths   []string
	present bool
	err     error
}

func (f *fakeFS) BrewExecutable(p string) (bool, error) {
	f.paths = append(f.paths, p)
	if p != "/opt/homebrew/bin/brew" && p != "/usr/local/bin/brew" {
		panic("unexpected FS probe")
	}
	return f.present, f.err
}

type fakeClock struct {
	durations []time.Duration
	fire      bool
	fireFirst int
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.durations = append(f.durations, d)
	c := make(chan time.Time, 1)
	if f.fire || (f.fireFirst > 0 && len(f.durations) <= f.fireFirst) {
		c <- time.Unix(0, 0)
	}
	return c
}

type fakeDetect struct {
	results map[string][]string
	calls   []string
}

func (f *fakeDetect) Detect(_ context.Context, i catalogue.Item) detect.Result {
	f.calls = append(f.calls, i.ID)
	states := f.results[i.ID]
	if len(states) == 0 {
		panic("unexpected detection: " + i.ID)
	}
	s := states[0]
	if len(states) > 1 {
		f.results[i.ID] = states[1:]
	}
	return detect.Result{Item: i.ID, State: s, Managed: s == "present_ok" || s == "present_outdated"}
}

type fakeJournal struct {
	events    []Event
	closed    int
	recordErr error
}

func (f *fakeJournal) Record(_ context.Context, e Event) error {
	f.events = append(f.events, e)
	return f.recordErr
}
func (f *fakeJournal) Close(context.Context) error { f.closed++; return nil }

type fakeBook struct {
	begun   int
	journal fakeJournal
	err     error
}

func (f *fakeBook) Begin(_ context.Context, hash string) (Journal, error) {
	if len(hash) != 64 {
		panic("unsealed plan")
	}
	f.begun++
	return &f.journal, f.err
}

type fakeNetwork struct {
	requests []LookupRequest
	err      error
}

func (f *fakeNetwork) Lookup(_ context.Context, r LookupRequest) (bool, error) {
	f.requests = append(f.requests, r)
	return f.err == nil, f.err
}
func item(id string) catalogue.Item {
	return catalogue.Item{ID: id, Kind: "tool", SelectedByDefault: true, Tool: catalogue.Tool{Command: id, MinVersion: "1.0.0", Source: catalogue.Source{Manager: "homebrew", Type: "formula", Name: id, Tap: "homebrew/core"}}}
} // TEST ONLY floor, not Mac-measured.
func fixture(t *testing.T, items ...catalogue.Item) (Engine, Request, *fakeExec, *fakeFS, *fakeDetect, *fakeBook, *fakeClock) {
	t.Helper()
	input := plan.Input{Platform: "darwin-arm64", Catalogue: &catalogue.Catalogue{SchemaVersion: 1, CatalogueVersion: "TESTONLY", Items: items}}
	d := &fakeDetect{results: map[string][]string{}}
	x := &fakeExec{t: t}
	for _, i := range items {
		input.Results = append(input.Results, detect.Result{Item: i.ID, State: "absent"})
		d.results[i.ID] = []string{"absent", "present_ok"}
		x.allowed = append(x.allowed, []string{"/opt/homebrew/bin/brew", "install", i.Tool.Source.Name})
	}
	p, err := plan.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFS{present: true}
	b := &fakeBook{}
	c := &fakeClock{}
	return Engine{Exec: x, FS: f, Detector: d, Clock: c, Bookkeeper: b, Network: &fakeNetwork{}}, Request{Input: input, ExpectedHash: p.Hash, Policy: Policy{Mode: "setup", Yes: true, UID: 501}, Environment: map[string]string{"HOME": "/fixture/home", "PASSWORD": "password-canary", "TOKEN": "token-canary"}}, x, f, d, b, c
}
func reseal(t *testing.T, r *Request) {
	t.Helper()
	p, err := plan.Build(r.Input)
	if err != nil {
		t.Fatal(err)
	}
	r.ExpectedHash = p.Hash
}
func TestBrewArgvAndEnvFixed(t *testing.T) {
	for _, cask := range []bool{false, true} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprint(cask, upgrade), func(t *testing.T) {
				i := item("jq")
				if cask {
					i.ID = "orbstack"
					i.Kind = "runtime"
					i.Tool.Source.Name = "orbstack"
					i.Tool.Source.Type = "cask"
					i.Tool.Source.Tap = "homebrew/cask"
				}
				e, r, x, _, d, _, _ := fixture(t, i)
				verb := "install"
				if upgrade {
					verb = "upgrade"
					r.Input.Results[0].State = "present_outdated"
					r.Input.Results[0].Managed = true
					r.Policy.Confirmed = map[string]bool{i.ID: true}
					d.results[i.ID] = []string{"present_outdated", "present_ok"}
					reseal(t, &r)
				}
				argv := []string{"/opt/homebrew/bin/brew", verb}
				if cask {
					argv = append(argv, "--cask")
				}
				argv = append(argv, i.Tool.Source.Name)
				x.allowed = [][]string{argv}
				got, err := e.Apply(context.Background(), r)
				if err != nil || len(got.Steps) != 1 || got.Steps[0].Status != "applied" {
					t.Fatalf("apply: %+v %v", got, err)
				}
				if len(x.calls) != 1 || !reflect.DeepEqual(x.calls[0].Argv, argv) {
					t.Fatalf("calls %+v", x.calls)
				}
				cmd := x.calls[0]
				if cmd.Stdin != "closed" || cmd.OutputLimit != 65536 || cmd.Env["HOMEBREW_NO_AUTO_UPDATE"] != "1" || cmd.Env["HOMEBREW_NO_ANALYTICS"] != "1" || cmd.Env["HOMEBREW_NO_ENV_HINTS"] != "1" || cmd.Env["PASSWORD"] != "" || cmd.Env["TOKEN"] != "" || cmd.Env["PATH"] != "/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin" {
					t.Fatalf("unsafe process policy: %+v", cmd)
				}
			})
		}
	}
}
func TestBrewMissingBlocksSteps(t *testing.T) {
	e, r, x, f, _, b, _ := fixture(t, item("jq"))
	f.present = false
	got, err := e.Apply(context.Background(), r)
	if err != nil || len(got.Steps) != 1 || got.Steps[0].Reason != "package_manager_missing" || len(x.calls) != 0 || b.begun != 1 {
		t.Fatalf("missing brew: %+v %v", got, err)
	}
}
func TestRootOrSudoStartRefused(t *testing.T) {
	for _, sudo := range []bool{false, true} {
		e, r, x, _, _, b, _ := fixture(t, item("jq"))
		r.Policy.UID = 0
		if sudo {
			r.Policy.UID = 501
			r.Policy.SUDOUserSet = true
		}
		_, err := e.Apply(context.Background(), r)
		if err == nil || len(x.calls) != 0 || b.begun != 0 {
			t.Fatal("root/sudo start reached effects")
		}
	}
}
func TestPackageManagerNoReinstallRetryAndVersionFloor(t *testing.T) {
	e, r, x, _, d, b, _ := fixture(t, item("jq"))
	d.results["jq"] = []string{"present_ok"}
	got, err := e.Apply(context.Background(), r)
	if err != nil || got.Steps[0].Status != "unchanged" || len(x.calls) != 0 || b.journal.closed != 1 {
		t.Fatalf("no reinstall %+v %v", got, err)
	}
	e, r, x, _, d, _, _ = fixture(t, item("jq"))
	x.exit = 1
	got, err = e.Apply(context.Background(), r)
	if err != nil || got.Steps[0].Status != "failed" || len(x.calls) != 1 {
		t.Fatalf("failed install %+v %v", got, err)
	}
	x.exit = 0
	d.results["jq"] = []string{"absent", "present_outdated"}
	got, err = e.Apply(context.Background(), r)
	if err != nil || got.Steps[0].Reason != "version_below_minimum" || !got.Steps[0].Changed || len(x.calls) != 2 {
		t.Fatalf("retry/floor %+v %v", got, err)
	}
}
func TestPackageManagerDependenciesAndSealedConfirmation(t *testing.T) {
	dep := item("dep")
	child := item("child")
	child.Requires = []string{"dep"}
	e, r, x, _, _, _, _ := fixture(t, dep, child, item("independent"))
	x.exit = 1
	got, err := e.Apply(context.Background(), r)
	if err != nil || len(x.calls) != 2 || got.Steps[1].Reason != "dependency_unavailable" {
		t.Fatalf("dependency isolation: %+v %v", got, err)
	}
	e, r, x, _, _, b, _ := fixture(t, item("jq"))
	r.Input.Catalogue.Items[0].Tool.Source.Name = "other"
	_, err = e.Apply(context.Background(), r)
	if err == nil || len(x.calls) != 0 || b.begun != 0 {
		t.Fatal("changed catalogue inherited confirmation")
	}
	e, r, x, _, _, b, _ = fixture(t, item("jq"))
	r.Policy.Yes = false
	_, err = e.Apply(context.Background(), r)
	if err == nil || len(x.calls) != 0 || b.begun != 0 {
		t.Fatal("unconfirmed run wrote protocol")
	}
}
func TestPackageManagerNoopAndProtocolPrecondition(t *testing.T) {
	e, r, x, _, _, b, _ := fixture(t, item("jq"))
	r.Input.OptedOut = map[string]bool{"jq": true}
	reseal(t, &r)
	got, err := e.Apply(context.Background(), r)
	if err != nil || len(got.Steps) != 0 || len(x.calls) != 0 || b.begun != 1 || b.journal.closed != 1 {
		t.Fatalf("confirmed no-op bookkeeping %+v %v", got, err)
	}
	for _, mode := range []string{"doctor", "dry-run"} {
		e, r, x, _, _, b, _ := fixture(t, item("jq"))
		if mode == "doctor" {
			r.Policy.Mode = "doctor"
		} else {
			r.Policy.DryRun = true
		}
		_, err = e.Apply(context.Background(), r)
		if err != nil || len(x.calls) != 0 || b.begun != 0 {
			t.Fatal("read-only mode reached effects")
		}
	}
	e, r, x, _, _, b, _ = fixture(t, item("jq"))
	b.err = errors.New("unwritable")
	_, err = e.Apply(context.Background(), r)
	if err == nil || len(x.calls) != 0 {
		t.Fatal("mutation without protocol")
	}
	e, r, x, _, _, b, _ = fixture(t, item("jq"))
	b.journal.recordErr = errors.New("append failed")
	_, err = e.Apply(context.Background(), r)
	if err == nil || len(x.calls) != 0 {
		t.Fatal("mutation without write-ahead record")
	}
}
func TestStepTimeoutInterruptsChild(t *testing.T) {
	for _, graceful := range []bool{false, true} {
		e, r, x, _, _, _, c := fixture(t, item("jq"))
		p := &fakeProcess{done: make(chan Completion, 1), finishOnInterrupt: graceful}
		x.processes = []*fakeProcess{p}
		c.fire = true
		got, err := e.Apply(context.Background(), r)
		if err != nil || len(x.calls) != 1 || p.interrupts != 1 || (!graceful && p.kills != 1) || got.Steps[0].Reason != "deadline_exceeded" {
			t.Fatalf("deadline %+v %v interrupts=%d kills=%d", got, err, p.interrupts, p.kills)
		}
		if !reflect.DeepEqual(c.durations, []time.Duration{900 * time.Second, 30 * time.Second}) {
			t.Fatalf("bounds %v", c.durations)
		}
	}
}
func TestPrivilegedStepShownConfirmedNeverUnattended(t *testing.T) {
	i := item("jq")
	argv := []string{"/usr/bin/fixture", "--readonly"}
	i.Privileged = []catalogue.Privileged{{Argv: argv}}
	good := Policy{Mode: "fix", Yes: true, Terminal: true, Confirmed: map[string]bool{"jq": true}, Shown: map[string][]string{"jq": {"/usr/bin/sudo", "--", "/usr/bin/fixture", "--readonly"}}}
	cmd, err := PrivilegedPreview(i, argv, good)
	if err != nil || !reflect.DeepEqual(cmd.Argv, good.Shown["jq"]) {
		t.Fatalf("exact preview %+v %v", cmd, err)
	}
	for _, which := range []string{"terminal", "json", "noninteractive", "confirmed", "shown", "doctor", "dry-run"} {
		p := good
		p.Confirmed = map[string]bool{"jq": true}
		p.Shown = good.Shown
		switch which {
		case "terminal":
			p.Terminal = false
		case "json":
			p.JSON = true
		case "noninteractive":
			p.NonInteractive = true
		case "confirmed":
			p.Confirmed = nil
		case "shown":
			p.Shown = nil
		case "doctor":
			p.Mode = "doctor"
		case "dry-run":
			p.DryRun = true
		}
		if _, err := PrivilegedPreview(i, argv, p); err == nil {
			t.Fatalf("accepted privilege %s", which)
		}
	}
	i.Privileged = []catalogue.Privileged{{Argv: []string{"/opt/homebrew/bin/brew", "install", "jq"}}}
	if _, err := PrivilegedPreview(i, i.Privileged[0].Argv, good); err == nil {
		t.Fatal("brew under sudo")
	}
}
func TestLookupsAreReadOnlyHTTPSOfDeclaredURLs(t *testing.T) {
	e, _, _, _, _, _, _ := fixture(t, item("jq"))
	n := &fakeNetwork{}
	e.Network = n
	i := item("jq")
	i.Network = catalogue.Network{Hosts: []string{"fixture.invalid"}, LookupURLs: []string{"https://fixture.invalid/version"}}
	ok, err := e.Lookup(context.Background(), i, i.Network.LookupURLs[0])
	if err != nil || !ok || len(n.requests) != 1 {
		t.Fatalf("lookup %v %v", ok, err)
	}
	q := n.requests[0]
	if q.Method != "GET" || q.UserAgent != LookupUserAgent || q.FollowRedirects || q.Timeout != 5*time.Second || q.BodyLimit != 65536 {
		t.Fatalf("unsafe lookup %+v", q)
	}
	for _, bad := range []string{"http://fixture.invalid/version", "https://other.invalid/version", "https://user:secret@fixture.invalid/version", "https://fixture.invalid/version?host=private"} {
		if _, err := e.Lookup(context.Background(), i, bad); err == nil {
			t.Fatalf("accepted undeclared URL %q", bad)
		}
	}
	if len(n.requests) != 1 {
		t.Fatal("invalid lookup reached network")
	}
	n.err = errors.New("private-response-canary")
	ok, err = e.Lookup(context.Background(), i, i.Network.LookupURLs[0])
	if ok || err != nil {
		t.Fatal("lookup failure not reported as unverified")
	}
}
func TestNoPasswordCanaryOrUnplannedRuntimeAction(t *testing.T) {
	i := item("orbstack")
	i.Kind = "runtime"
	i.Tool.Source.Type = "cask"
	i.Tool.Source.Tap = "homebrew/cask"
	e, r, x, _, _, b, _ := fixture(t, i)
	x.allowed = [][]string{{"/opt/homebrew/bin/brew", "install", "--cask", "orbstack"}}
	got, err := e.Apply(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	rendered := fmt.Sprintf("%+v %+v %+v", got, x.calls, b.journal.events)
	for _, secret := range []string{"password-canary", "token-canary"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("secret escaped: %s", secret)
		}
	}
	if len(x.calls) != 1 {
		t.Fatal("unplanned runtime start/config")
	}
}

func TestStepCancellationAndStartDeadline(t *testing.T) {
	e, r, x, _, _, _, _ := fixture(t, item("jq"), item("other"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &fakeProcess{done: make(chan Completion, 1), finishOnInterrupt: true}
	x.processes = []*fakeProcess{p}
	x.onStart = func(startCtx context.Context) {
		deadline, ok := startCtx.Deadline()
		if !ok || time.Until(deadline) > 900*time.Second {
			t.Error("Start lacks bounded context")
		}
		cancel()
	}
	got, err := e.Apply(ctx, r)
	if err != nil || len(x.calls) != 1 || p.interrupts != 1 || p.kills != 0 || got.Steps[0].Reason != "cancelled" || got.Steps[1].Status != "skipped" {
		t.Fatalf("cancelled child: %+v %v", got, err)
	}
}
func TestRuntimeOnlyWhenSelectedNeverStarted(t *testing.T) {
	for _, off := range []string{"unselected", "optedout", "doctor", "dryrun"} {
		i := item("orbstack")
		i.Kind = "runtime"
		i.Tool.Source.Type = "cask"
		i.Tool.Source.Tap = "homebrew/cask"
		e, r, x, _, _, _, _ := fixture(t, i)
		switch off {
		case "unselected":
			r.Input.Selected = map[string]bool{i.ID: false}
		case "optedout":
			r.Input.OptedOut = map[string]bool{i.ID: true}
		case "doctor":
			r.Policy.Mode = "doctor"
		case "dryrun":
			r.Policy.DryRun = true
		}
		reseal(t, &r)
		if _, err := e.Apply(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if len(x.calls) != 0 {
			t.Fatalf("runtime action in %s", off)
		}
	}
}
func TestNoLoginShellStartupFileTapOrTelemetry(t *testing.T) {
	e, r, x, f, _, _, _ := fixture(t, item("jq"))
	n := &fakeNetwork{}
	e.Network = n
	if _, err := e.Apply(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(x.calls) != 1 || !reflect.DeepEqual(x.calls[0].Argv, []string{"/opt/homebrew/bin/brew", "install", "jq"}) || len(n.requests) != 0 || !reflect.DeepEqual(f.paths, []string{"/opt/homebrew/bin/brew"}) {
		t.Fatal("unexpected shell/startup/tap/telemetry effect")
	}
}
func TestBrewRejectsForeignSourcesAndUnconfirmedUpgrade(t *testing.T) {
	for _, bad := range []string{"tap", "url", "cask", "elevation", "foreign", "upgrade"} {
		i := item("jq")
		e, r, x, _, d, _, _ := fixture(t, i)
		switch bad {
		case "tap":
			r.Input.Catalogue.Items[0].Tool.Source.Tap = "attacker/tap"
		case "url":
			r.Input.Catalogue.Items[0].Tool.Source.Name = "https://fixture.invalid/tool"
		case "cask":
			r.Input.Catalogue.Items[0].Tool.Source.Type = "cask"
		case "elevation":
			r.Input.Catalogue.Items[0].Tool.Source.MayElevate = true
		case "foreign":
			d.results[i.ID] = []string{"present_other_source"}
		case "upgrade":
			r.Input.Results[0].State = "present_outdated"
			r.Input.Results[0].Managed = true
			d.results[i.ID] = []string{"present_outdated"}
		}
		reseal(t, &r)
		got, err := e.Apply(context.Background(), r)
		if err != nil || len(x.calls) != 0 || len(got.Steps) != 1 || (got.Steps[0].Status != "blocked" && got.Steps[0].Status != "declined") {
			t.Fatalf("unsafe %s: %+v %v", bad, got, err)
		}
	}
}

func TestStepDeadlineBlocksOnlyDependents(t *testing.T) {
	first := item("first")
	dependent := item("dependent")
	dependent.Requires = []string{first.ID}
	e, r, x, _, _, _, clock := fixture(t, first, dependent, item("independent"))
	child := &fakeProcess{done: make(chan Completion, 1)}
	x.processes = []*fakeProcess{child}
	clock.fireFirst = 2
	got, err := e.Apply(context.Background(), r)
	if err != nil || len(got.Steps) != 3 {
		t.Fatalf("step results: %+v %v", got, err)
	}
	if got.Steps[0].Status != "failed" || got.Steps[0].Reason != "deadline_exceeded" || child.interrupts != 1 || child.kills != 1 {
		t.Fatalf("timed out step: %+v", got.Steps[0])
	}
	if got.Steps[1].Status != "blocked" || got.Steps[1].Reason != "dependency_unavailable" {
		t.Fatalf("dependent: %+v", got.Steps[1])
	}
	if got.Steps[2].Status != "applied" || len(x.calls) != 2 {
		t.Fatalf("independent work did not continue: %+v; calls=%d", got.Steps, len(x.calls))
	}
	if !reflect.DeepEqual(clock.durations, []time.Duration{900 * time.Second, 30 * time.Second, 900 * time.Second}) {
		t.Fatalf("step bounds: %v", clock.durations)
	}
}
