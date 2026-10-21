//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package native

import (
	"context"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakePackageChild struct {
	mu              sync.Mutex
	started, waited bool
	signals         []int
	exited          bool
}

func (c *fakePackageChild) Start() error { c.started = true; return nil }
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
	if len(cmd.Env) != len(packageCommand().Env) {
		t.Fatal("inherited environment")
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
