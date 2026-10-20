//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

// A controlled parent reports cancellation/deadline without sleeping or real
// timers. Err and Done remain safe for context.WithTimeout's propagation goroutine.
type boundaryContext struct {
	context.Context
	mu   sync.Mutex
	done chan struct{}
	err  error
}

func newBoundaryContext() *boundaryContext {
	return &boundaryContext{Context: context.Background(), done: make(chan struct{})}
}
func (c *boundaryContext) Done() <-chan struct{} { return c.done }
func (c *boundaryContext) Err() error            { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *boundaryContext) stop(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
}

type boundaryClock struct {
	now       time.Time
	remaining int
	fire      func()
}

func (c *boundaryClock) Now() time.Time {
	if c.remaining > 0 {
		c.remaining--
		if c.remaining == 0 {
			c.fire()
		}
	}
	return c.now
}
func checkBoundaryInterruption(t *testing.T, boundary string, cause error, changed bool) {
	t.Helper()
	e, r, runner, home := applyFixture(t)
	ctx := newBoundaryContext()
	defer ctx.stop(context.Canceled)
	clock := &boundaryClock{now: e.Clock.Now(), fire: func() { ctx.stop(cause) }}
	e.Clock = clock
	if boundary == "zero-step-begin" {
		r.Input.Results[0].State = "present_ok"
		resealApply(t, &r)
		// The first clock call supplies Begin's timestamp, after initial ctx.Err.
		clock.remaining = 1
	} else {
		runner.callback = func(_ context.Context, s Step, _ string, _ time.Duration) (StepOutcome, error) {
			// After this runner, the first Now measures elapsed time and the second
			// timestamps journalOutcome, after the final runner cancellation check.
			clock.remaining = 2
			audit := profile.StepAudit{Result: "unchanged"}
			if changed {
				audit.Result = "applied"
				audit.Argv = []string{"/opt/homebrew/bin/brew", "install", s.Item}
			}
			return StepOutcome{Audit: audit, Changed: changed}, nil
		}
	}
	out, err := e.Apply(ctx, r)
	want := 72
	if cause == context.DeadlineExceeded {
		want = 71
	}
	if changed {
		want = 73
	}
	if err != nil || out.ExitCode != want {
		t.Fatalf("%s cancellation misclassified: exit=%d want=%d err=%v", boundary, out.ExitCode, want, err)
	}
	if out.ProfileChanged {
		t.Fatal("cancelled run persisted profile")
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("cancelled run changed managed profile", err)
	}
	wantCalls := 1
	if boundary == "zero-step-begin" {
		wantCalls = 0
	}
	if len(runner.calls) != wantCalls {
		t.Fatal("unexpected action count", runner.calls)
	}
	raw := readApply(t, filepath.Join(home, ".local/state/whr/protocols/"+r.RunID+".jsonl"))
	var end map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		if record["type"] == "run_end" {
			end = record
		}
	}
	if end == nil || end["exit_code"] != float64(want) {
		t.Fatal("run_end disagrees with interrupted result", end)
	}
	next, err := e.Store.Begin(profile.Start{RunID: "1123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, clock.Now())
	if err != nil {
		t.Fatal("interrupted boundary retained lock", err)
	}
	next.Close()
}
func TestApplyInterruptionDuringZeroStepBegin(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) { checkBoundaryInterruption(t, "zero-step-begin", cause, false) })
	}
}
func TestApplyInterruptionDuringFinalStepAudit(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, changed := range []bool{false, true} {
			name := cause.Error() + "/unchanged"
			if changed {
				name = cause.Error() + "/applied"
			}
			t.Run(name, func(t *testing.T) { checkBoundaryInterruption(t, "final-step-audit", cause, changed) })
		}
	}
}
