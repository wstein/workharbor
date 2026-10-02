package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/termproto"
)

// fakeTTY is a terminal a test types into and reads from.
type fakeTTY struct {
	in      *io.PipeReader
	inW     *io.PipeWriter
	mu      sync.Mutex
	out     bytes.Buffer
	raw     int
	restore int
	cols    uint16
	rows    uint16
	resize  chan struct{}
}

func newFakeTTY() *fakeTTY {
	r, w := io.Pipe()
	return &fakeTTY{in: r, inW: w, cols: 100, rows: 30, resize: make(chan struct{}, 1)}
}

func (f *fakeTTY) Reader() io.Reader { return f.in }
func (f *fakeTTY) Writer() io.Writer { return &lockedWriter{f} }
func (f *fakeTTY) MakeRaw() (func(), error) {
	f.mu.Lock()
	f.raw++
	f.mu.Unlock()
	return func() { f.mu.Lock(); f.restore++; f.mu.Unlock() }, nil
}

func (f *fakeTTY) Size() (uint16, uint16, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows, nil
}
func (f *fakeTTY) Resizes() (<-chan struct{}, func()) { return f.resize, func() {} }
func (f *fakeTTY) output() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.String()
}

type lockedWriter struct{ f *fakeTTY }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	return w.f.out.Write(p)
}

// runCLIWithTTY runs whr with a terminal.
func (s *stub) runCLIWithTTY(tty TTY, args ...string) (code int, stdout, stderr string) {
	s.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Getenv: func(k string) string {
			if k == "TERM" {
				return "xterm-256color"
			}
			return ""
		},
		NewClient: func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil },
		TTY:       func() (TTY, error) { return tty, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code = Execute(ctx, env, args)
	return code, out.String(), errOut.String()
}

// shellStub answers the upgrade like the supervisor does: it shouts what it reads,
// records resizes, and ends with exit code 3 when it reads "exit".
func (s *stub) shellStub(frames *[]termproto.Frame, mu *sync.Mutex) {
	s.mu.Lock()
	s.h["GET /v1/console/shell"] = func(w http.ResponseWriter, _ *http.Request, _ string) {
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			s.t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+termproto.Upgrade+"\r\n\r\n")
		for {
			f, err := termproto.Read(buf.Reader)
			if err != nil {
				return
			}
			mu.Lock()
			*frames = append(*frames, f)
			mu.Unlock()
			switch {
			case f.Type == termproto.TypeData && bytes.Contains(f.Data, []byte("exit")):
				_ = termproto.Write(conn, termproto.ExitFrame(3))
				return
			case f.Type == termproto.TypeData:
				_ = termproto.Write(conn, termproto.Frame{Type: termproto.TypeData, Data: bytes.ToUpper(f.Data)})
			}
		}
	}
	s.mu.Unlock()
}

func TestConsoleOpensASessionAndCarriesTheTerminal(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/console", 200, ok(`{"env_id":"env-1","read_write":["docs-ws"],"reused":false}`))
	var frames []termproto.Frame
	var mu sync.Mutex
	s.shellStub(&frames, &mu)
	tty := newFakeTTY()
	go func() {
		_, _ = io.WriteString(tty.inW, "hello\n")
		time.Sleep(100 * time.Millisecond)
		tty.mu.Lock()
		tty.cols, tty.rows = 120, 50
		tty.mu.Unlock()
		tty.resize <- struct{}{}
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(tty.inW, "exit\n")
	}()
	code, out, errOut := s.runCLIWithTTY(tty, "console", "docs-ws", "--write")
	if code != 3 {
		t.Fatalf("exit %d, want the shell's 3; stderr %q", code, errOut)
	}
	if out != "" {
		t.Errorf("stdout = %q: the terminal's output goes to the terminal, not to stdout", out)
	}
	if !strings.Contains(tty.output(), "HELLO") {
		t.Errorf("terminal output = %q", tty.output())
	}
	for _, want := range []string{"opening the console", "console open (workspaces read-write: docs-ws)"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	tty.mu.Lock()
	raw, restored := tty.raw, tty.restore
	tty.mu.Unlock()
	if raw != 1 || restored != 1 {
		t.Errorf("raw mode set %d times, restored %d: the terminal must always be given back", raw, restored)
	}
	// The request: writable workspace, the terminal's size, TERM and workspace.
	open := s.requests("POST /v1/console")
	if len(open) != 1 || !strings.Contains(open[0].body, `"read_write":["docs-ws"]`) || open[0].header.Get("Idempotency-Key") == "" {
		t.Errorf("open request = %+v", open)
	}
	shell := s.requests("GET /v1/console/shell")
	if len(shell) != 1 {
		t.Fatalf("shell requests = %+v", shell)
	}
	q, _ := url.ParseQuery(shell[0].query)
	if q.Get("cols") != "100" || q.Get("rows") != "30" || q.Get("term") != "xterm-256color" || q.Get("workspace") != "docs-ws" {
		t.Errorf("shell query = %v", q)
	}
	if shell[0].header.Get("Authorization") != "Bearer "+tok || !strings.EqualFold(shell[0].header.Get("Upgrade"), termproto.Upgrade) {
		t.Errorf("shell headers = %v", shell[0].header)
	}
	mu.Lock()
	defer mu.Unlock()
	var sawResize bool
	for _, f := range frames {
		if f.Type == termproto.TypeResize {
			if c, r, err := termproto.ParseResize(f); err == nil && c == 120 && r == 50 {
				sawResize = true
			}
		}
	}
	if !sawResize {
		t.Errorf("the resize did not reach the supervisor: %+v", frames)
	}
}

func TestConsoleReadOnlyByDefaultAndOnlyWithATerminal(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/console", 200, ok(`{"env_id":"env-1","read_write":[],"reused":true}`))
	var frames []termproto.Frame
	var mu sync.Mutex
	s.shellStub(&frames, &mu)
	tty := newFakeTTY()
	go func() { _, _ = io.WriteString(tty.inW, "exit\n") }()
	if code, _, _ := s.runCLIWithTTY(tty, "console"); code != 3 {
		t.Fatalf("exit %d", code)
	}
	if open := s.requests("POST /v1/console"); !strings.Contains(open[0].body, `"read_write":[]`) {
		t.Errorf("a console without --write must open nothing writable: %s", open[0].body)
	}
	if shell := s.requests("GET /v1/console/shell"); strings.Contains(shell[0].query, "workspace=") {
		t.Errorf("no workspace named, none sent: %s", shell[0].query)
	}
	// With no terminal, nothing is opened.
	s2 := newStub(t)
	code, _, errOut := s2.runCLI("", "console")
	if code != exitcode.Usage || !strings.Contains(errOut, "needs a terminal") || len(s2.requests("POST /v1/console")) != 0 {
		t.Errorf("without a terminal: exit %d, %q, requests %v", code, errOut, s2.requests("POST /v1/console"))
	}
	for _, args := range [][]string{{"console", "--write"}, {"console", "--close", "--status"}, {"console", "docs", "--status"}, {"console", "--close", "--write", "docs"}, {"console", "a", "b"}} {
		if code, _, _ := s2.runCLIWithTTY(newFakeTTY(), args...); code != exitcode.Usage {
			t.Errorf("%v: exit %d, want usage", args, code)
		}
	}
}

func TestConsoleSaysWhatTheSupervisorRefuses(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/console", 409, fail("conflict", 5, "the console is open with other writable workspaces: close it first"))
	code, _, errOut := s.runCLIWithTTY(newFakeTTY(), "console", "docs", "--write")
	if code != exitcode.Conflict || !strings.Contains(errOut, "close it first") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if len(s.requests("GET /v1/console/shell")) != 0 {
		t.Error("a shell was asked for after the console was refused")
	}
	// A shell the supervisor refuses before the upgrade is an ordinary error, and the
	// terminal is not left in raw mode.
	s2 := newStub(t)
	s2.reply("POST /v1/console", 200, ok(`{"env_id":"e","read_write":[],"reused":false}`))
	s2.reply("GET /v1/console/shell", 409, fail("conflict", 5, "the console is not open: open it first"))
	tty := newFakeTTY()
	code, _, errOut = s2.runCLIWithTTY(tty, "console")
	if code != exitcode.Conflict || !strings.Contains(errOut, "open it first") || tty.raw != 0 {
		t.Errorf("exit %d, stderr %q, raw %d", code, errOut, tty.raw)
	}
}

func TestConsoleStatusAndClose(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/console", 200, ok(`null`))
	code, out, errOut := s.runCLI("", "console", "--status")
	if code != 0 || out != "" || !strings.Contains(errOut, "no console is open") {
		t.Errorf("none: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	s.reply("GET /v1/console", 200, ok(`{"env_id":"env-1","read_write":["a","b"],"reused":true}`))
	code, out, _ = s.runCLI("", "console", "--status")
	if code != 0 || !strings.Contains(out, "env-1") || !strings.Contains(out, "a,b") {
		t.Errorf("open: exit %d, stdout %q", code, out)
	}
	s.reply("DELETE /v1/console", 200, ok(`{}`))
	code, _, errOut = s.runCLI("", "console", "--close")
	if code != 0 || !strings.Contains(errOut, "closed") || len(s.requests("DELETE /v1/console")) != 1 {
		t.Errorf("close: exit %d, stderr %q", code, errOut)
	}
}
