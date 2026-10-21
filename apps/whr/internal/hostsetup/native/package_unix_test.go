//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package native

import (
	"context"
	"errors"
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type fakePackageChild struct {
	mu              sync.Mutex
	started, waited bool
	signals         []int
	exited          bool
	completion      *pkgmgr.Completion
}

func (c *fakePackageChild) Start(context.Context) error { c.started = true; return nil }
func (c *fakePackageChild) Exited() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exited, nil
}
func (c *fakePackageChild) Signal(s int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signals = append(c.signals, s)
	if s == int(syscall.SIGKILL) {
		c.exited = true
	}
	return nil
}
func (c *fakePackageChild) Wait() pkgmgr.Completion {
	c.waited = true
	if c.completion != nil {
		return *c.completion
	}
	return pkgmgr.Completion{Exit: 0}
}
func (c *fakePackageChild) Release() error { return nil }
func packageCommand() pkgmgr.Command {
	return pkgmgr.Command{Argv: []string{"/opt/homebrew/bin/brew", "install", "jq"}, Env: map[string]string{"HOME": "/tmp/home", "PATH": "/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin", "HOMEBREW_NO_AUTO_UPDATE": "1", "HOMEBREW_NO_ANALYTICS": "1", "HOMEBREW_NO_ENV_HINTS": "1"}, Stdin: "closed", OutputLimit: 65536}
}
func TestPackageCommandAdapterClosedGrammar(t *testing.T) {
	calls := 0
	adapter := PackageExec{launch: func(c pkgmgr.Command) (packageChild, error) { calls++; return nil, errors.New("fake launch") }}
	if _, err := adapter.Start(context.Background(), packageCommand()); err == nil || calls != 1 {
		t.Fatalf("valid command not offered to fake launcher: calls=%d err=%v", calls, err)
	}
	for _, argv := range [][]string{{"/usr/bin/sudo", "--", "/opt/homebrew/bin/brew", "install", "jq"}, {"/bin/sh", "-c", "brew install jq"}, {"/opt/homebrew/bin/brew", "update"}, {"/opt/homebrew/bin/brew", "install", "--HEAD", "jq"}} {
		c := packageCommand()
		c.Argv = argv
		if _, err := adapter.Start(context.Background(), c); err == nil {
			t.Fatalf("accepted forbidden argv %q", argv)
		}
	}
	if calls != 1 {
		t.Fatal("forbidden command reached launcher")
	}
}
func TestPackageCommandAdapterReapsBeforeDone(t *testing.T) {
	child := &fakePackageChild{exited: true}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
	p, err := adapter.Start(context.Background(), packageCommand())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("fake child not completed")
	}
	if !child.started || !child.waited || len(child.signals) != 1 {
		t.Fatal("Done preceded cleanup/reap")
	}
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if len(child.signals) != 1 {
		t.Fatal("signalled reaped child")
	}
}

func TestPackageCommandAdapterRejectsEnvironmentAndTerminal(t *testing.T) {
	calls := 0
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { calls++; return nil, errors.New("must not launch") }}
	mutations := []func(*pkgmgr.Command){
		func(c *pkgmgr.Command) { c.Env["SECRET"] = "password-canary" },
		func(c *pkgmgr.Command) { c.Env["PATH"] = "/tmp/untrusted" },
		func(c *pkgmgr.Command) { c.Env["HOMEBREW_NO_AUTO_UPDATE"] = "0" },
		func(c *pkgmgr.Command) { c.Env["HOME"] = "relative" },
		func(c *pkgmgr.Command) { c.Stdin = "terminal" },
		func(c *pkgmgr.Command) { c.OutputLimit = 0 },
	}
	for _, mutate := range mutations {
		c := packageCommand()
		mutate(&c)
		if _, err := adapter.Start(context.Background(), c); err == nil {
			t.Fatal("accepted changed command contract")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Start(ctx, packageCommand()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup: %v", err)
	}
	if calls != 0 {
		t.Fatal("forbidden startup reached launch")
	}
}
func TestNativePackageCommandDoesNotStartOrInherit(t *testing.T) {
	child, err := newPackageChild(packageCommand())
	if err != nil {
		t.Fatal(err)
	}
	cmd := child.(*nativePackageChild).cmd
	if cmd.Process != nil {
		t.Fatal("constructor started real process")
	}
	if cmd.Path != "/opt/homebrew/bin/brew" || cmd.Stdin != nil || cmd.Dir != "/" || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.WaitDelay != 100*time.Millisecond {
		t.Fatal("native command boundary changed")
	}
	if !reflect.DeepEqual(cmd.Args, packageCommand().Argv) {
		t.Fatalf("argv fidelity: %q", cmd.Args)
	}
	wantEnv := packageCommand().Env
	gotEnv := map[string]string{}
	for _, entry := range cmd.Env {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatal("bad environment")
		}
		if _, duplicate := gotEnv[k]; duplicate {
			t.Fatal("duplicate environment key")
		}
		gotEnv[k] = v
	}
	if !reflect.DeepEqual(gotEnv, wantEnv) {
		t.Fatalf("environment fidelity: %#v", gotEnv)
	}
	if cmd.Stdout != io.Discard || cmd.Stderr != io.Discard {
		t.Fatal("native output retained")
	}

}

func TestPackageCommandAdapterEngineOwnsCancellation(t *testing.T) {
	child := &fakePackageChild{}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	p, err := adapter.Start(ctx, packageCommand())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err = p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
		t.Fatal("startup cancellation bypassed engine grace")
	default:
	}
	if err = p.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("fake child not reaped after kill")
	}
	if !child.waited || len(child.signals) != 3 || child.signals[0] != int(syscall.SIGINT) || child.signals[1] != int(syscall.SIGKILL) {
		t.Fatalf("unexpected owned signals: %v", child.signals)
	}
	if err = p.Kill(); err != nil {
		t.Fatal(err)
	}
	if len(child.signals) != 3 {
		t.Fatal("Kill signalled after reap")
	}
}

type gatedReapChild struct {
	fakePackageChild
	waiting     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func (c *gatedReapChild) releaseStart() { c.releaseOnce.Do(func() { close(c.release) }) }
func (c *gatedReapChild) Wait() pkgmgr.Completion {
	close(c.waiting)
	<-c.release
	return c.fakePackageChild.Wait()
}
func TestPackageCommandAdapterKillWaitsForReap(t *testing.T) {
	child := &gatedReapChild{waiting: make(chan struct{}), release: make(chan struct{})}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
	p, err := adapter.Start(context.Background(), packageCommand())
	if err != nil {
		t.Fatal(err)
	}
	killed := make(chan error, 1)
	go func() { killed <- p.Kill() }()
	select {
	case <-child.waiting:
	case <-time.After(time.Second):
		child.releaseStart()
		t.Fatal("owned cleanup never reached Wait")
	}
	returnedEarly := false
	select {
	case <-killed:
		returnedEarly = true
	case <-time.After(30 * time.Millisecond):
	}
	child.releaseStart()
	if !returnedEarly {
		select {
		case err = <-killed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Kill failed to finish after reap")
		}
	}
	var completion pkgmgr.Completion
	select {
	case completion = <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("completion not delivered after reap")
	}
	if returnedEarly {
		t.Fatal("Kill returned before owned cleanup/reap")
	}
	if completion.Exit != 0 {
		t.Fatalf("completion %v", completion)
	}
}
func TestPackageCommandAdapterInputIsolation(t *testing.T) {
	original := packageCommand()
	expected := packageCommand()
	adapter := PackageExec{launch: func(c pkgmgr.Command) (packageChild, error) {
		if !reflect.DeepEqual(c, expected) {
			t.Fatalf("launcher command changed: %#v", c)
		}
		c.Argv[1] = "update"
		c.Env["HOME"] = "/tmp/changed"
		return nil, errors.New("inert launch")
	}}
	_, _ = adapter.Start(context.Background(), original)
	if !reflect.DeepEqual(original, expected) {
		t.Fatalf("caller command aliased: %#v", original)
	}
}
func TestPackageCommandAdapterCancellationDuringLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	child := &fakePackageChild{}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { cancel(); return child, nil }}
	p, err := adapter.Start(ctx, packageCommand())
	if p != nil || !errors.Is(err, context.Canceled) || child.started {
		t.Fatalf("launched after cancellation: p=%v err=%v started=%v", p, err, child.started)
	}
}

type cooperativeStartChild struct {
	fakePackageChild
	entered     chan struct{}
	release     chan struct{}
	lateSuccess bool
	releaseOnce sync.Once
}

func (c *cooperativeStartChild) releaseStart() { c.releaseOnce.Do(func() { close(c.release) }) }

func (c *cooperativeStartChild) Start(ctx context.Context) error {
	close(c.entered)
	if c.lateSuccess {
		<-c.release
		c.started = true
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.release:
		c.started = true
		return nil
	}
}

// This context advances a logical deadline only after the fake port enters;
// no scheduler delay can expire a real timer before the entry observation.
type controlledStartupContext struct {
	context.Context
	mu       sync.Mutex
	done     chan struct{}
	err      error
	deadline bool
}

func (c *controlledStartupContext) Done() <-chan struct{}       { return c.done }
func (c *controlledStartupContext) Err() error                  { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *controlledStartupContext) Deadline() (time.Time, bool) { return time.Unix(123, 0), c.deadline }
func (c *controlledStartupContext) expire(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
}
func TestPackageCommandAdapterCooperativeStartupCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline=%t", deadline), func(t *testing.T) {
			child := &cooperativeStartChild{entered: make(chan struct{}), release: make(chan struct{})}
			adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
			ctx := &controlledStartupContext{Context: context.Background(), done: make(chan struct{}), deadline: deadline}
			result := make(chan error, 1)
			joined := false
			go func() {
				p, err := adapter.Start(ctx, packageCommand())
				if p != nil {
					_ = p.Kill()
				}
				result <- err
			}()
			t.Cleanup(func() {
				child.releaseStart()
				if !joined {
					select {
					case <-result:
					case <-time.After(time.Second):
						t.Error("startup goroutine survived release")
					}
				}
			})
			select {
			case <-child.entered:
			case err := <-result:
				joined = true
				t.Fatalf("startup returned before controlled expiry: %v", err)
			case <-time.After(time.Second):
				t.Fatal("fake startup never entered")
			}
			want := context.Canceled
			if deadline {
				want = context.DeadlineExceeded
			}
			ctx.expire(want)
			select {
			case err := <-result:
				joined = true
				if !errors.Is(err, want) || child.started {
					t.Fatalf("startup outcome: %v started=%v", err, child.started)
				}
			case <-time.After(200 * time.Millisecond):
				t.Fatal("cooperative startup ignored context")
			}
		})
	}
}
func TestPackageCommandAdapterLateStartupSuccessIsReaped(t *testing.T) {
	child := &cooperativeStartChild{entered: make(chan struct{}), release: make(chan struct{}), lateSuccess: true}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		p, err := adapter.Start(ctx, packageCommand())
		if p != nil {
			_ = p.Kill()
		}
		result <- err
	}()
	select {
	case <-child.entered:
	case <-time.After(time.Second):
		child.releaseStart()
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Error("late startup goroutine survived release")
		}
		t.Fatal("late startup never entered")
	}
	cancel()
	child.releaseStart()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("late result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("late start cleanup did not complete")
	}
	if !child.waited || len(child.signals) == 0 || child.signals[len(child.signals)-1] != int(syscall.SIGKILL) {
		t.Fatalf("late child not cleaned/reaped: %v", child.signals)
	}
}
func TestPackageCommandAdapterCompletionFidelity(t *testing.T) {
	sentinel := errors.New("fake exit")
	child := &fakePackageChild{exited: true, completion: &pkgmgr.Completion{Exit: 77, Err: sentinel}}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
	p, err := adapter.Start(context.Background(), packageCommand())
	if err != nil {
		t.Fatal(err)
	}
	var completion pkgmgr.Completion
	select {
	case completion = <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("completion not delivered")
	}
	if completion.Exit != 77 || !errors.Is(completion.Err, sentinel) || !child.waited {
		t.Fatalf("completion changed: %#v", completion)
	}
	if len(child.signals) != 1 || child.signals[0] != int(syscall.SIGKILL) {
		t.Fatalf("wrong final signal: %v", child.signals)
	}
	select {
	case _, ok := <-p.Done():
		if ok {
			t.Fatal("completion channel not closed")
		}
	case <-time.After(time.Second):
		t.Fatal("completion channel remained open")
	}
	if err = p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err = p.Kill(); err != nil {
		t.Fatal(err)
	}
	if len(child.signals) != 1 {
		t.Fatal("signal after reap")
	}
}

type enginePackageFS struct{}

func (enginePackageFS) BrewExecutable(string) (bool, error) { return true, nil }

type enginePackageClock struct{ calls int }

func (c *enginePackageClock) After(time.Duration) <-chan time.Time {
	c.calls++
	ch := make(chan time.Time, 1)
	if c.calls <= 2 {
		ch <- time.Now()
	}
	return ch
}

type enginePackageDetector struct{ seen map[string]int }

func (d enginePackageDetector) Detect(_ context.Context, i catalogue.Item) detect.Result {
	d.seen[i.ID]++
	state := "absent"
	if d.seen[i.ID] > 1 {
		state = "present_ok"
	}
	return detect.Result{Item: i.ID, State: state}
}

type enginePackageBook struct {
	closed              chan struct{}
	closeErr, recordErr error
}

func (b enginePackageBook) Begin(context.Context, string) (pkgmgr.Journal, error) { return b, nil }
func (b enginePackageBook) Record(_ context.Context, event pkgmgr.Event) error {
	if event.Stage == "finished" {
		return b.recordErr
	}
	return nil
}
func (b enginePackageBook) Close(context.Context) error { close(b.closed); return b.closeErr }
func TestPackageCommandAdapterEngineCannotOutrunReap(t *testing.T) {
	child := &gatedReapChild{waiting: make(chan struct{}), release: make(chan struct{})}
	var launches atomic.Int32
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) {
		if launches.Add(1) == 1 {
			return child, nil
		}
		return &fakePackageChild{exited: true}, nil
	}}
	items := []catalogue.Item{}
	for _, id := range []string{"jq", "fzf"} {
		items = append(items, catalogue.Item{ID: id, SelectedByDefault: true, Tool: catalogue.Tool{Source: catalogue.Source{Manager: "homebrew", Type: "formula", Tap: "homebrew/core", Name: id}}})
	}
	input := plan.Input{Platform: "darwin-arm64", Catalogue: &catalogue.Catalogue{SchemaVersion: 1, Items: items}, Results: []detect.Result{{Item: "jq", State: "absent"}, {Item: "fzf", State: "absent"}}}
	sealed, err := plan.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	book := enginePackageBook{closed: make(chan struct{})}
	engine := pkgmgr.Engine{Exec: adapter, FS: enginePackageFS{}, Clock: &enginePackageClock{}, Detector: enginePackageDetector{seen: map[string]int{}}, Bookkeeper: book}
	completed := make(chan error, 1)
	go func() {
		_, err := engine.Apply(context.Background(), pkgmgr.Request{Input: input, ExpectedHash: sealed.Hash, Policy: pkgmgr.Policy{Mode: "setup", Yes: true, UID: 501}, Environment: map[string]string{"HOME": "/tmp/home"}})
		completed <- err
	}()
	select {
	case <-child.waiting:
	case <-time.After(time.Second):
		child.releaseStart()
		t.Fatal("engine never waited for child cleanup")
	}
	early := launches.Load() != 1
	select {
	case <-book.closed:
		early = true
	default:
	}
	select {
	case <-completed:
		early = true
	default:
	}
	child.releaseStart()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("engine did not finish after child cleanup")
	}
	if early {
		t.Fatal("engine advanced independent work or closed bookkeeping before reap")
	}
	if launches.Load() != 2 {
		t.Fatalf("independent second step not executed: %d", launches.Load())
	}
	select {
	case <-book.closed:
	default:
		t.Fatal("bookkeeping not closed")
	}
}

type signalFailureChild struct {
	fakePackageChild
	failAt   int
	cancel   context.CancelFunc
	waitDone chan struct{}
	waitOnce sync.Once
}

func (c *signalFailureChild) Start(context.Context) error {
	c.started = true
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}
func (c *signalFailureChild) Wait() pkgmgr.Completion {
	result := c.fakePackageChild.Wait()
	if c.waitDone != nil {
		c.waitOnce.Do(func() { close(c.waitDone) })
	}
	return result
}
func (c *signalFailureChild) Signal(sig int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signals = append(c.signals, sig)
	if len(c.signals) == c.failAt {
		return syscall.EPERM
	}
	if sig == int(syscall.SIGKILL) {
		c.exited = true
	}
	return nil
}
func TestPackageCommandAdapterKillPreservesCleanupFailure(t *testing.T) {
	child := &signalFailureChild{failAt: 2}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
	p, err := adapter.Start(context.Background(), packageCommand())
	if err != nil {
		t.Fatal(err)
	}
	killErr := p.Kill()
	var completion pkgmgr.Completion
	select {
	case completion = <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("cleanup outcome unavailable")
	}
	var owned pkgmgr.CleanupError
	if !errors.Is(killErr, syscall.EPERM) || !errors.As(killErr, &owned) || owned.CleanupProcess() != p || !errors.Is(completion.Err, syscall.EPERM) {
		t.Fatalf("cleanup failure erased: Kill=%v Done=%v", killErr, completion.Err)
	}
	before := len(child.signals)
	if err = p.Kill(); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("terminal error erased: %v", err)
	}
	if len(child.signals) != before {
		t.Fatal("retry signalled after terminal Wait")
	}
}
func TestPackageCommandAdapterLateStartupCleanupFailureKeepsOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	child := &signalFailureChild{failAt: 1, cancel: cancel, waitDone: make(chan struct{})}
	adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
	type startOutcome struct {
		process pkgmgr.Process
		err     error
	}
	outcome := make(chan startOutcome, 1)
	go func() { p, err := adapter.Start(ctx, packageCommand()); outcome <- startOutcome{p, err} }()
	var got startOutcome
	select {
	case got = <-outcome:
	case <-time.After(200 * time.Millisecond):
		child.mu.Lock()
		child.exited = true
		child.mu.Unlock()
		select {
		case late := <-outcome:
			if late.process != nil {
				_ = late.process.Kill()
			}
		case <-time.After(time.Second):
			t.Error("failed startup goroutine survived inert child exit")
		}
		t.Fatal("cleanup failure did not return owned startup outcome")
	}
	p, err := got.process, got.err

	// Always complete the inert child, even when the ownership assertion fails.
	if p == nil {
		child.mu.Lock()
		child.exited = true
		child.mu.Unlock()
		select {
		case <-child.waitDone:
		case <-time.After(time.Second):
			t.Error("inert failure child did not finish cleanup")
		}
		t.Fatalf("late live child lost: %v", err)
	}
	defer func() {
		if retry := p.Kill(); retry != nil {
			t.Error(retry)
		}
	}()
	var owned pkgmgr.CleanupError
	if !errors.Is(err, context.Canceled) || !errors.Is(err, syscall.EPERM) || !errors.As(err, &owned) || owned.CleanupProcess() != p {
		t.Fatalf("late cleanup evidence/ownership lost: %v", err)
	}
}

func TestPackageCommandAdapterEngineCleanupFailureRetainsOwnership(t *testing.T) {
	for _, late := range []bool{false, true} {
		for _, journalFailure := range []string{"none", "record", "close"} {
			t.Run(fmt.Sprintf("late=%t/journal=%s", late, journalFailure), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				child := &signalFailureChild{failAt: 3, waitDone: make(chan struct{})}
				if late {
					child.failAt = 1
					child.cancel = cancel
				}
				launches := 0
				adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) {
					launches++
					if launches > 1 {
						return &fakePackageChild{exited: true}, nil
					}
					return child, nil
				}}
				items := []catalogue.Item{}
				for _, id := range []string{"jq", "fzf"} {
					items = append(items, catalogue.Item{ID: id, SelectedByDefault: true, Tool: catalogue.Tool{Source: catalogue.Source{Manager: "homebrew", Type: "formula", Tap: "homebrew/core", Name: id}}})
				}
				input := plan.Input{Platform: "darwin-arm64", Catalogue: &catalogue.Catalogue{SchemaVersion: 1, Items: items}, Results: []detect.Result{{Item: "jq", State: "absent"}, {Item: "fzf", State: "absent"}}}
				sealed, err := plan.Build(input)
				if err != nil {
					t.Fatal(err)
				}
				journalErr := errors.New("inert journal failure")
				book := enginePackageBook{closed: make(chan struct{})}
				if journalFailure == "record" {
					book.recordErr = journalErr
				}
				if journalFailure == "close" {
					book.closeErr = journalErr
				}
				engine := pkgmgr.Engine{Exec: adapter, FS: enginePackageFS{}, Clock: &enginePackageClock{}, Detector: enginePackageDetector{seen: map[string]int{}}, Bookkeeper: book}
				type applyOutcome struct {
					result pkgmgr.Result
					err    error
				}
				outcomes := make(chan applyOutcome, 1)
				go func() {
					result, err := engine.Apply(ctx, pkgmgr.Request{Input: input, ExpectedHash: sealed.Hash, Policy: pkgmgr.Policy{Mode: "setup", Yes: true, UID: 501}, Environment: map[string]string{"HOME": "/tmp/home"}})
					outcomes <- applyOutcome{result, err}
				}()
				var outcome applyOutcome
				select {
				case outcome = <-outcomes:
				case <-time.After(200 * time.Millisecond):
					child.mu.Lock()
					child.exited = true
					child.mu.Unlock()
					select {
					case late := <-outcomes:
						var owned pkgmgr.CleanupError
						if errors.As(late.err, &owned) {
							_ = owned.CleanupProcess().Kill()
						}
					case <-time.After(time.Second):
						t.Error("Apply goroutine survived inert child exit")
					}
					t.Fatal("cleanup failure did not return owned Apply outcome")
				}
				result, err := outcome.result, outcome.err
				var owned pkgmgr.CleanupError
				if !errors.As(err, &owned) {
					child.mu.Lock()
					child.exited = true
					child.mu.Unlock()
					select {
					case <-child.waitDone:
					case <-time.After(time.Second):
					}
					t.Fatalf("Apply lost owned cleanup failure: %v", err)
				}
				process := owned.CleanupProcess()
				defer func() {
					retryErr := process.Kill()
					if late && retryErr != nil {
						t.Error(retryErr)
					}
					select {
					case <-process.Done():
					case <-time.After(time.Second):
						t.Error("owned child not joined")
					}
				}()
				if !errors.Is(err, syscall.EPERM) || late && !errors.Is(err, context.Canceled) {
					t.Fatalf("cleanup cause lost: %v", err)
				}
				if journalFailure != "none" && !strings.Contains(err.Error(), "host_setup.protocol_unwritable") {
					t.Fatalf("journal failure lost cleanup cause: %v", err)
				}
				if len(result.Steps) != 1 || result.Steps[0].Status != "failed" || !result.Steps[0].MutationAttempted || result.Steps[0].Changed || launches != 1 {
					t.Fatalf("uncertain mutation misreported or continuation occurred: %+v launches=%d", result, launches)
				}
				select {
				case <-process.(*packageProcess).observerDone:
				case <-time.After(time.Second):
					t.Fatal("failed cleanup retained idle observer")
				}
				select {
				case <-book.closed:
				default:
					t.Fatal("bookkeeping not closed")
				}
			})
		}
	}
}
