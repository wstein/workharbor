// SPDX-License-Identifier: EUPL-1.2

package sandbox_test

import (
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/sandbox"
)

func TestStateTransitionTable(t *testing.T) {
	type S = sandbox.State
	// The table of specs/008-workspace-sandbox/data-model.md, row by row.
	legal := map[[2]S]bool{
		{sandbox.StateNone, sandbox.StatePreparing}:               true,
		{sandbox.StatePreparing, sandbox.StateReady}:              true,
		{sandbox.StatePreparing, sandbox.StateFailed}:             true,
		{sandbox.StateReady, sandbox.StateRunning}:                true,
		{sandbox.StateRunning, sandbox.StateFinished}:             true,
		{sandbox.StateRunning, sandbox.StateFailed}:               true,
		{sandbox.StateReady, sandbox.StateDiscarding}:             true,
		{sandbox.StateFinished, sandbox.StateDiscarding}:          true,
		{sandbox.StateFailed, sandbox.StateDiscarding}:            true,
		{sandbox.StateDiscarding, sandbox.StateDiscarded}:         true,
		{sandbox.StateDiscarding, sandbox.StateCleanupIncomplete}: true,
		{sandbox.StateCleanupIncomplete, sandbox.StateDiscarding}: true,
	}
	all := sandbox.States()
	if len(all) != 9 {
		t.Fatalf("the none state and eight lifecycle states, got %v", all)
	}
	names := map[S]string{}
	for _, s := range all {
		names[s] = string(s)
	}
	for _, want := range []string{"", "preparing", "ready", "running", "finished", "failed", "discarding", "discarded", "cleanup_incomplete"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("state %q missing", want)
		}
	}
	for _, from := range all {
		for _, to := range all {
			next, err := sandbox.Transition(from, to)
			if legal[[2]S{from, to}] {
				if err != nil || next != to {
					t.Errorf("%q -> %q must be legal: %v", from, to, err)
				}
				continue
			}
			code, ok := runtime.RefusalCodeOf(err)
			if !ok || code != runtime.RefusedInvalidState {
				t.Errorf("%q -> %q must be refused.invalid_state, got %v", from, to, err)
			}
			if next != from {
				t.Errorf("%q -> %q: a refused transition must leave the state at %q, got %q", from, to, from, next)
			}
		}
	}
	// A state outside the model is never a source or a target.
	for _, bad := range []sandbox.State{"RUNNING", "paused", "discard", " ready"} {
		if _, err := sandbox.Transition(bad, sandbox.StateReady); err == nil {
			t.Errorf("unknown source %q accepted", bad)
		}
		if _, err := sandbox.Transition(sandbox.StateReady, bad); err == nil {
			t.Errorf("unknown target %q accepted", bad)
		}
	}
	// Terminal and export rules.
	if sandbox.StateDiscarded.CanLeave() {
		t.Error("discarded is terminal")
	}
	for _, s := range all {
		wantExport := s == sandbox.StateFinished || s == sandbox.StateFailed
		if s.ExportAllowed() != wantExport {
			t.Errorf("%q ExportAllowed = %v", s, s.ExportAllowed())
		}
	}
}
