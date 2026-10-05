package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// failingReader fails the test if anything reads it: nothing the human types
// belongs to whr.
type failingReader struct{ t *testing.T }

func (r failingReader) Read([]byte) (int, error) {
	r.t.Error("whr read the terminal's input")
	return 0, errors.New("read")
}

// wsShellRun runs `whr ws shell` with a fake terminal and a fake child, and a fake
// container CLI on PATH. It returns what child was started.
type execCall struct {
	bin  string
	argv []string
}

func wsShellRun(t *testing.T, s *stub, terminal bool, args ...string) (code int, calls []execCall, errOut string, tty *fakeTTY) {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // a fake executable must be executable
	if err := os.WriteFile(filepath.Join(dir, "container"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	tty = newFakeTTY()
	in := failingReader{t}
	var out, eb bytes.Buffer
	childScope, restored := false, false
	env := Env{
		Stdin: in, Stdout: &out, Stderr: &eb,
		Getenv: func(k string) string {
			if k == "TERM" {
				return "xterm-256color"
			}
			return ""
		},
		NewClient: func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil },
		ShellSignals: func() func() {
			childScope = true
			return func() { childScope = false; restored = true }
		},
		ShellChild: func(_ context.Context, bin string, argv, _ []string) (int, error) {
			if !childScope {
				t.Error("command SIGINT cancellation was not suppressed for the child")
			}
			calls = append(calls, execCall{bin, argv})
			return 0, nil
		},
	}
	if terminal {
		env.TTY = func() (TTY, error) { return tty, nil }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code = Execute(ctx, env, append([]string{"ws", "shell"}, args...))
	if len(calls) != 0 && (!restored || childScope) {
		t.Error("command signal scope was not restored after child exit")
	}
	return code, calls, eb.String(), tty
}

const shellReply = `{"env_id":"whr-abc","runtime":"apple-container","user":"1000:1000","dir":"/home/workharbor","env":["HOME=/home/workharbor","CLAUDE_CONFIG_DIR=/home/workharbor/.claude","HTTPS_PROXY=http://192.168.64.3:3128"],"cmd":["/bin/sh","-c","exec sh -i","whr-shell","/tools/bin"]}`

// Nothing typed or written is logged or stored by whr: whr asks the supervisor one
// question, then starts the runtime's child with the inherited
// terminal, and never reads, raw-modes or writes it.
func TestWsShellStartsTheRuntimesChildAndTouchesNoTerminalByte(t *testing.T) {
	s := newStub(t)
	shellReplyHeld(s, shellReply)
	code, calls, errOut, tty := wsShellRun(t, s, true, "docs-ws")
	if code != 0 || len(calls) != 1 {
		t.Fatalf("exit %d, %d exec calls, stderr %q", code, len(calls), errOut)
	}
	argv := calls[0].argv
	for _, want := range []string{"exec", "-t", "-i", "-u", "1000:1000", "-w", "/home/workharbor", "whr-abc"} {
		if !slices.Contains(argv, want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
	for _, want := range []string{"CLAUDE_CONFIG_DIR=/home/workharbor/.claude", "HTTPS_PROXY=http://192.168.64.3:3128", "TERM=xterm-256color"} {
		if !slices.Contains(argv, want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
	if joined := strings.Join(argv, " "); strings.Contains(joined, tok) || strings.Contains(joined, "--env-file") {
		t.Errorf("argv carries the API token or an env file: %q", argv)
	}
	if n := len(s.requests("POST /v1/workspaces/docs-ws/shell")); n != 1 {
		t.Errorf("%d shell requests, want 1", n)
	}
	s.mu.Lock()
	total := len(s.req)
	s.mu.Unlock()
	if total != 1 {
		t.Errorf("whr made %d API requests, want only the metadata hold: no terminal relay", total)
	}
	if tty.raw != 0 || tty.output() != "" {
		t.Errorf("whr put the terminal in raw mode %d times or wrote %q to it", tty.raw, tty.output())
	}
	if !strings.Contains(errOut, "claude auth login") {
		t.Errorf("stderr %q does not say what to run", errOut)
	}
}

func TestWsShellIsRefusedBySupervisorAndExecsNothing(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/workspaces/busy/shell", 409, fail("conflict", 5, "workspace busy has run r1 (running) of agent a1: finish, stop or fail it first"))
	code, calls, errOut, _ := wsShellRun(t, s, true, "busy")
	if code == 0 || len(calls) != 0 || !strings.Contains(errOut, "r1") {
		t.Errorf("exit %d, %d exec calls, stderr %q", code, len(calls), errOut)
	}
}

func TestWsShellNeedsATerminalBeforeItAsksAnything(t *testing.T) {
	s := newStub(t)
	shellReplyHeld(s, shellReply)
	code, calls, _, _ := wsShellRun(t, s, false, "docs-ws")
	if code == 0 || len(calls) != 0 {
		t.Errorf("exit %d, %d exec calls without a terminal", code, len(calls))
	}
	if n := len(s.requests("POST /v1/workspaces/docs-ws/shell")); n != 0 {
		t.Errorf("a request was sent without a terminal: %d", n)
	}
}

func TestWsShellRefusesARuntimeItDoesNotKnow(t *testing.T) {
	s := newStub(t)
	shellReplyHeld(s, strings.Replace(shellReply, "apple-container", "docker", 1))
	if code, calls, _, _ := wsShellRun(t, s, true, "docs-ws"); code == 0 || len(calls) != 0 {
		t.Errorf("exit %d, %d exec calls for an unknown runtime", code, len(calls))
	}
}

func TestWsShellRefusesAUserOrDirThatLooksLikeAnOption(t *testing.T) {
	for name, reply := range map[string]string{
		"user": strings.Replace(shellReply, `"1000:1000"`, `"--privileged"`, 1),
		"dir":  strings.Replace(shellReply, `"/home/workharbor"`, `"-v"`, 1),
	} {
		s := newStub(t)
		shellReplyHeld(s, reply)
		if code, calls, _, _ := wsShellRun(t, s, true, "docs-ws"); code == 0 || len(calls) != 0 {
			t.Errorf("%s: exit %d, %d exec calls", name, code, len(calls))
		}
	}
}

func shellReplyHeld(s *stub, reply string) {
	s.h["POST /v1/workspaces/docs-ws/shell"] = func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.Header.Get("Accept") != "application/x-ndjson" {
			s.t.Error("missing lifetime hold request")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, ok(reply))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
}

func TestWsShellEndsTheChildWhenTheHoldEndsFirst(t *testing.T) {
	s := newStub(t)
	childStarted := make(chan struct{})
	s.h["POST /v1/workspaces/docs-ws/shell"] = func(w http.ResponseWriter, _ *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, ok(shellReply))
		w.(http.Flusher).Flush()
		<-childStarted // EOF while child runs
	}
	dir := t.TempDir()
	//nolint:gosec // test executable must be executable
	if err := os.WriteFile(filepath.Join(dir, "container"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var out bytes.Buffer
	ended := false
	code := Execute(t.Context(), Env{
		Stdin: failingReader{t}, Stdout: &out, Stderr: &out, Getenv: func(string) string { return "" },
		TTY:       func() (TTY, error) { return newFakeTTY(), nil },
		NewClient: func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil },
		ShellChild: func(ctx context.Context, _ string, _, _ []string) (int, error) {
			close(childStarted)
			<-ctx.Done()
			ended = true
			return 0, ctx.Err()
		},
	}, []string{"ws", "shell", "docs-ws"})
	if code == 0 || !ended || !strings.Contains(out.String(), "hold ended") {
		t.Fatalf("exit %d, ended %v, output %q", code, ended, out.String())
	}
}

func TestShellHoldRefusesAnOldSupervisorWithoutLifetimeProtocol(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/workspaces/docs-ws/shell", 200, ok(shellReply))
	code, calls, _, _ := wsShellRun(t, s, true, "docs-ws")
	if code == 0 || len(calls) != 0 {
		t.Fatalf("old supervisor started child: exit %d, calls %d", code, len(calls))
	}
}
