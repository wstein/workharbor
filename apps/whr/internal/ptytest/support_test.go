// SPDX-License-Identifier: EUPL-1.2

package ptytest_test

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/ptytest"
)

// fakeTB lets a test observe the harness failing without failing itself.
type fakeTB struct {
	testing.TB
	dir      string
	fatals   []string
	skips    []string
	cleanups []func()
	done     chan struct{}
}

func newFakeTB(t *testing.T) *fakeTB {
	return &fakeTB{TB: t, dir: t.TempDir(), done: make(chan struct{})}
}

func (f *fakeTB) Helper()             {}
func (f *fakeTB) TempDir() string     { return f.dir }
func (f *fakeTB) Cleanup(fn func())   { f.cleanups = append(f.cleanups, fn) }
func (f *fakeTB) Logf(string, ...any) {}
func (f *fakeTB) Errorf(format string, a ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, a...))
}
func (f *fakeTB) Fatalf(format string, a ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, a...))
	runtime.Goexit()
}
func (f *fakeTB) Skipf(format string, a ...any) {
	f.skips = append(f.skips, fmt.Sprintf(format, a...))
}

// run executes fn on its own goroutine so Fatalf's Goexit ends only that.
func (f *fakeTB) run(fn func()) {
	go func() {
		defer close(f.done)
		fn()
	}()
	select {
	case <-f.done:
	case <-time.After(watchdog):
		f.TB.Fatalf("harness call hung for %v: a deadline is not enforced", watchdog)
	}
}

// watchdog turns a hung harness call (a mutant that ignores a deadline or
// never kills a child) into a prompt failure instead of the go test timeout.
const watchdog = 30 * time.Second

func (f *fakeTB) cleanup() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := len(f.cleanups) - 1; i >= 0; i-- {
			f.cleanups[i]()
		}
	}()
	select {
	case <-done:
	case <-time.After(watchdog):
		f.TB.Fatalf("cleanup hung for %v", watchdog)
	}
	f.cleanups = nil
}

func (f *fakeTB) wantFatal(sub string) {
	f.TB.Helper()
	for _, m := range f.fatals {
		if strings.Contains(m, sub) {
			return
		}
	}
	f.TB.Fatalf("no failure mentioning %q; got %q", sub, f.fatals)
}

// waitGone fails unless the process (not a zombie) disappears. It polls
// because process death is observable only through the OS.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ptytest.ProcessGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d still running after cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPTYUnsupportedPlatformSkipsExplicitly(t *testing.T) {
	f := newFakeTB(t)
	if ptytest.SkipUnlessSupported(f, false, "plan9") {
		t.Fatal("unsupported platform reported as supported")
	}
	if len(f.skips) != 1 || !strings.Contains(f.skips[0], "SKIPPED, NOT PASSED") ||
		!strings.Contains(f.skips[0], "plan9") {
		t.Fatalf("skip message = %q", f.skips)
	}
	g := newFakeTB(t)
	if !ptytest.SkipUnlessSupported(g, true, "linux") || len(g.skips) != 0 {
		t.Fatal("supported platform must not skip")
	}
	// The documented platforms must really be supported.
	if !ptytest.Supported("linux") || !ptytest.Supported("darwin") || ptytest.Supported("windows") {
		t.Fatal("documented platform list changed")
	}
	if os.Getenv("PTYTEST_FUNC") != "" {
		t.Fatal("fixture variable leaked into the test process")
	}
}

// guard runs fn and fails the test, instead of hanging until the go test
// timeout, if it does not return within the watchdog.
func guard(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(watchdog):
		t.Fatalf("%s hung for %v", what, watchdog)
	}
}
