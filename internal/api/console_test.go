package api

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/termproto"
)

// echoTerminal is a terminal that shouts what is typed and ends, with code 5,
// when it reads "exit".
type echoTerminal struct {
	mu     sync.Mutex
	out    *io.PipeReader
	outW   *io.PipeWriter
	sizes  [][2]uint16
	typed  bytes.Buffer
	closed bool
	ended  chan struct{}
	once   sync.Once
}

func newEchoTerminal() *echoTerminal {
	r, w := io.Pipe()
	return &echoTerminal{out: r, outW: w, ended: make(chan struct{})}
}

func (e *echoTerminal) Read(p []byte) (int, error) { return e.out.Read(p) }

func (e *echoTerminal) Write(p []byte) (int, error) {
	e.mu.Lock()
	e.typed.Write(p)
	e.mu.Unlock()
	if bytes.Contains(p, []byte("exit")) {
		e.finish()
		return len(p), nil
	}
	_, err := e.outW.Write(bytes.ToUpper(p))
	return len(p), err
}

func (e *echoTerminal) Resize(cols, rows uint16) error {
	e.mu.Lock()
	e.sizes = append(e.sizes, [2]uint16{cols, rows})
	e.mu.Unlock()
	return nil
}

func (e *echoTerminal) finish() {
	e.once.Do(func() { _ = e.outW.Close(); close(e.ended) })
}

func (e *echoTerminal) Close() error {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.finish()
	return nil
}

func (e *echoTerminal) Wait() (int, error) { <-e.ended; return 5, nil }

func (e *echoTerminal) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// upgrade sends the shell request over a raw connection, with the token unless
// auth is false, and returns the connection, a reader on it and the response.
func (r *rig) upgrade(query string, auth bool, extra ...string) (net.Conn, *bufio.Reader, int, http.Header) {
	r.t.Helper()
	var d net.Dialer
	conn, err := d.DialContext(context.Background(), "tcp", r.ts.Listener.Addr().String())
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = conn.Close() })
	var b strings.Builder
	b.WriteString("GET /v1/console/shell" + query + " HTTP/1.1\r\nHost: whr\r\n")
	if auth {
		b.WriteString("Authorization: Bearer " + token + "\r\n")
	}
	for _, h := range extra {
		b.WriteString(h + "\r\n")
	}
	b.WriteString("\r\n")
	if _, err := io.WriteString(conn, b.String()); err != nil {
		r.t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	_ = resp.Body.Close() // the status and headers are what the tests read; the connection closes with the test
	return conn, br, resp.StatusCode, resp.Header
}

var shellHeaders = []string{"Connection: Upgrade", "Upgrade: " + termproto.Upgrade}

func TestTheShellIsBehindTheTokenAndNeedsTheUpgrade(t *testing.T) {
	r := newRig(t)
	r.be.onShell = func(service.ShellRequest) (runtime.Terminal, error) { return newEchoTerminal(), nil }
	_, _, status, _ := r.upgrade("", false, shellHeaders...)
	if status != http.StatusUnauthorized || len(r.be.shells) != 0 {
		t.Errorf("without the token: %d, shells %d", status, len(r.be.shells))
	}
	for name, hdr := range map[string][]string{"no upgrade": nil, "wrong protocol": {"Connection: Upgrade", "Upgrade: websocket"}, "no connection header": {"Upgrade: " + termproto.Upgrade}} {
		_, _, status, _ := r.upgrade("", true, hdr...)
		if status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, status)
		}
	}
	for _, q := range []string{"?cols=0", "?rows=x", "?cols=70000", "?workspace=" + strings.Repeat("a", 300)} {
		_, _, status, _ := r.upgrade(q, true, shellHeaders...)
		if status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", q, status)
		}
	}
	if len(r.be.shells) != 0 {
		t.Errorf("a refused request reached the console: %+v", r.be.shells)
	}
	// Whatever the console refuses is an ordinary error, before any upgrade.
	r.be.onShell = nil
	if _, _, status, _ := r.upgrade("", true, shellHeaders...); status != http.StatusConflict {
		t.Errorf("a console that is not open: %d, want 409", status)
	}
}

func TestAShellIsAStreamOfFramesBothWays(t *testing.T) {
	r := newRig(t)
	tm := newEchoTerminal()
	r.be.onShell = func(service.ShellRequest) (runtime.Terminal, error) { return tm, nil }
	conn, br, status, hdr := r.upgrade("?cols=100&rows=30&term=xterm-kitty&workspace=docs-ws", true, shellHeaders...)
	if status != http.StatusSwitchingProtocols || !strings.EqualFold(hdr.Get("Upgrade"), termproto.Upgrade) {
		t.Fatalf("status %d, upgrade %q", status, hdr.Get("Upgrade"))
	}
	r.be.mu.Lock()
	asked := r.be.shells[0]
	r.be.mu.Unlock()
	if asked.Workspace != "docs-ws" || asked.Term != "xterm-kitty" || asked.Cols != 100 || asked.Rows != 30 || asked.Actor != "api" {
		t.Errorf("request = %+v", asked)
	}

	if err := termproto.Write(conn, termproto.Frame{Type: termproto.TypeData, Data: []byte("hello\\n")}); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for !strings.Contains(got.String(), "HELLO") {
		f, err := termproto.Read(br)
		if err != nil || f.Type != termproto.TypeData {
			t.Fatalf("frame %+v, %v after %q", f, err, got.String())
		}
		got.Write(f.Data)
	}
	if err := termproto.Write(conn, termproto.ResizeFrame(120, 50)); err != nil {
		t.Fatal(err)
	}
	if err := termproto.Write(conn, termproto.Frame{Type: termproto.TypeData, Data: []byte("exit\\n")}); err != nil {
		t.Fatal(err)
	}
	// The shell ended: the last frame is its exit code, then the stream ends.
	for {
		f, err := termproto.Read(br)
		if err != nil {
			t.Fatalf("the stream ended without an exit frame: %v", err)
		}
		if f.Type == termproto.TypeExit {
			if code, err := termproto.ParseExit(f); err != nil || code != 5 {
				t.Fatalf("exit = %d, %v", code, err)
			}
			break
		}
	}
	if _, err := termproto.Read(br); err == nil {
		t.Error("the stream went on after the exit frame")
	}
	tm.mu.Lock()
	sizes := append([][2]uint16(nil), tm.sizes...)
	tm.mu.Unlock()
	if len(sizes) != 1 || sizes[0] != [2]uint16{120, 50} {
		t.Errorf("sizes = %v", sizes)
	}
	if !tm.isClosed() {
		t.Error("the terminal was not closed when the stream ended")
	}
}

// A client that goes away hangs the terminal up, and so does one that sends a
// frame it may not send.
func TestAClientThatLeavesOrMisbehavesClosesTheTerminal(t *testing.T) {
	for name, act := range map[string]func(net.Conn){
		"disconnects": func(c net.Conn) { _ = c.Close() },
		"sends an exit frame": func(c net.Conn) {
			_ = termproto.Write(c, termproto.ExitFrame(0))
		},
		"sends a frame that is too big": func(c net.Conn) {
			_, _ = c.Write([]byte{termproto.TypeData, 0xff, 0xff, 0xff, 0xff})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			tm := newEchoTerminal()
			r.be.onShell = func(service.ShellRequest) (runtime.Terminal, error) { return tm, nil }
			conn, _, status, _ := r.upgrade("", true, shellHeaders...)
			if status != http.StatusSwitchingProtocols {
				t.Fatalf("status %d", status)
			}
			act(conn)
			deadline := time.Now().Add(5 * time.Second)
			for !tm.isClosed() {
				if time.Now().After(deadline) {
					t.Fatal("the terminal was left open")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
