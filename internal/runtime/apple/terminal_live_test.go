//go:build applecontainer

package apple

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
)

// collector reads a terminal into a buffer a test can poll.
type collector struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *collector) copy(t runtime.Terminal) {
	b := make([]byte, 4096)
	for {
		n, err := t.Read(b)
		c.mu.Lock()
		c.buf.Write(b[:n])
		c.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func (c *collector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func waitFor(t *testing.T, c *collector, want string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !strings.Contains(c.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in the terminal's output:\n%s", want, c.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestTerminalLive runs a command with a terminal in a real environment: the
// command sees a terminal of the size asked for, a resize reaches it, input
// arrives, and the exit code comes back.
//
//	go test -tags applecontainer -run TestTerminalLive ./internal/runtime/apple
func TestTerminalLive(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	ta, ok := h.Adapter.(runtime.TerminalAdapter)
	if !ok {
		t.Fatal("the Apple adapter has no terminal")
	}
	tm, err := ta.Terminal(ctx, id, runtime.TerminalRequest{
		Cmd:  []string{"sh", "-c", "echo TERM=$TERM; test -t 0 && echo ISTTY; stty size; read x; echo got:$x; stty size; exit 7"},
		Env:  []string{"TERM=xterm-256color"},
		Cols: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out collector
	go out.copy(tm)
	waitFor(t, &out, "TERM=xterm-256color")
	waitFor(t, &out, "ISTTY")
	waitFor(t, &out, "30 100")
	if err := tm.Resize(120, 50); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // the resize travels to the guest
	if _, err := tm.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &out, "got:hello")
	waitFor(t, &out, "50 120")
	code, err := tm.Wait()
	if err != nil || code != 7 {
		t.Errorf("exit code = %d, %v; want 7", code, err)
	}
	if err := tm.Close(); err != nil {
		t.Error(err)
	}
}

// Closing a terminal hangs the guest's terminal up, so a shell does not outlive it.
func TestTerminalCloseEndsTheGuestCommand(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	tm, err := h.Adapter.(runtime.TerminalAdapter).Terminal(ctx, id, runtime.TerminalRequest{Cmd: []string{"sh", "-c", "echo up; exec sleep 600"}, Env: []string{"TERM=xterm"}})
	if err != nil {
		t.Fatal(err)
	}
	var out collector
	go out.copy(tm)
	waitFor(t, &out, "up")
	_ = tm.Close()
	// The sleep is gone from the guest within a few seconds.
	deadline := time.Now().Add(20 * time.Second)
	for {
		st, err := h.Adapter.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"sh", "-c", "pgrep -x sleep >/dev/null && echo alive || echo gone"}})
		if err != nil {
			t.Fatal(err)
		}
		stdout, _, _, _ := runtime.Collect(st)
		if strings.TrimSpace(string(stdout)) == "gone" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the guest command outlived its terminal")
		}
		time.Sleep(500 * time.Millisecond)
	}
}
