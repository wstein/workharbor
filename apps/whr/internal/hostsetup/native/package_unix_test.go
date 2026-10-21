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
	waiting chan struct{}
	release chan struct{}
}

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
		close(child.release)
		t.Fatal("owned cleanup never reached Wait")
	}
	returnedEarly := false
	select {
	case <-killed:
		returnedEarly = true
	case <-time.After(30 * time.Millisecond):
	}
	close(child.release)
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
	completion := <-p.Done()
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
}

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
func TestPackageCommandAdapterCooperativeStartupCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline=%t", deadline), func(t *testing.T) {
			child := &cooperativeStartChild{entered: make(chan struct{}), release: make(chan struct{})}
			adapter := PackageExec{launch: func(pkgmgr.Command) (packageChild, error) { return child, nil }}
			ctx, cancel := context.WithCancel(context.Background())
			var stop context.CancelFunc
			if deadline {
				ctx, stop = context.WithTimeout(ctx, 30*time.Millisecond)
				defer stop()
			}
			defer cancel()
			result := make(chan error, 1)
			go func() {
				p, err := adapter.Start(ctx, packageCommand())
				if p != nil {
					_ = p.Kill()
				}
				result <- err
			}()
			<-child.entered
			want := context.Canceled
			if deadline {
				want = context.DeadlineExceeded
			} else {
				cancel()
			}
			var err error
			timely := true
			select {
			case err = <-result:
			case <-time.After(200 * time.Millisecond):
				timely = false
			}
			close(child.release)
			if !timely {
				select {
				case <-result:
				case <-time.After(time.Second):
					t.Fatal("startup goroutine survived release")
				}
				t.Fatal("cooperative startup ignored context")
			}
			if !errors.Is(err, want) || child.started {
				t.Fatalf("startup outcome: err=%v started=%v", err, child.started)
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
	<-child.entered
	cancel()
	close(child.release)
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
	if _, ok := <-p.Done(); ok {
		t.Fatal("completion channel not closed")
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

type enginePackageBook struct{ closed chan struct{} }

func (b enginePackageBook) Begin(context.Context, string) (pkgmgr.Journal, error) { return b, nil }
func (b enginePackageBook) Record(context.Context, pkgmgr.Event) error            { return nil }
func (b enginePackageBook) Close(context.Context) error                           { close(b.closed); return nil }
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
		close(child.release)
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
	close(child.release)
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
