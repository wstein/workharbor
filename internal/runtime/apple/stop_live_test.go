//go:build applecontainer

package apple

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
)

// TestStopLeavesNoProcessFromBeforeLive measures what design 4.1 ("No surviving
// agent before a relaunch", issue #216) rests on: a process started in an
// environment, detached from its exec client as an agent left behind by a
// crashed supervisor is (spike #7, Case 4), is gone after a stop and a new start
// of the environment. The Case 4 reproduction with a stop in place of the kill
// of the client.
//
//	go test -tags applecontainer -run TestStopLeavesNoProcessFromBeforeLive ./internal/runtime/apple
func TestStopLeavesNoProcessFromBeforeLive(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	id, err := h.Adapter.Provision(ctx, mustPrepare(t, h, h.NewSpec()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.Adapter.Stop(context.Background(), id)
		_ = h.Adapter.Delete(context.Background(), id)
	})
	if err := h.Adapter.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	probe := func(cmd string) string {
		t.Helper()
		st, err := h.Adapter.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"sh", "-c", cmd}})
		if err != nil {
			t.Fatal(err)
		}
		out, _, _, _ := runtime.Collect(st)
		return strings.TrimSpace(string(out))
	}
	// The marker is in the process's command line, so a new process cannot be
	// mistaken for it by a reused PID.
	const find = "for f in /proc/[0-9]*/cmdline; do tr '\\0' ' ' < $f 2>/dev/null; echo; done | grep -c '[s]leep 31313'"
	probe("nohup sleep 31313 >/dev/null 2>&1 </dev/null &")
	if got := probe(find); got != "1" {
		t.Fatalf("before the stop: %q processes with the marker, want 1 (the probe itself is wrong)", got)
	}
	if err := h.Adapter.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := h.Adapter.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	// Exec answers some 100 ms after a start (spike #2).
	deadline := time.Now().Add(30 * time.Second)
	for {
		if st, err := h.Adapter.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"true"}}); err == nil {
			if _, _, code, werr := runtime.Collect(st); werr == nil && code == 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the environment did not answer exec after the start")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := probe(find); got != "0" {
		t.Errorf("after the stop and start: %q processes with the marker, want 0", got)
	}
}
