package runtimetest

import (
	"context"
	"io"
	"sync"

	"github.com/wstein/workharbor/internal/runtime"
)

// Terminal implements runtime.TerminalAdapter on top of the fake's own exec: the
// terminal's input is the command's standard input, its output the command's
// stdout and stderr in one stream, and a resize is only recorded (Sizes). So
// `cat` is a terminal that echoes what is typed, `echo hi` prints and ends, and
// `exit 3` ends with that code.
func (f *Fake) Terminal(ctx context.Context, id string, req runtime.TerminalRequest) (runtime.Terminal, error) {
	in, inW := io.Pipe()
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	st, err := f.Exec(runCtx, id, runtime.ExecRequest{Cmd: req.Cmd, Env: req.Env, Dir: req.Dir, Stdin: in})
	if err != nil {
		cancel()
		_ = inW.Close()
		return nil, err
	}
	f.mu.Lock()
	f.terminals = append(f.terminals, TerminalCall{Env: id, Req: req})
	f.mu.Unlock()
	t := &fakeTerminal{f: f, id: id, in: inW, st: st, cancel: cancel, out: make(chan []byte, 16), done: make(chan struct{})}
	go func() {
		defer close(t.out)
		for c := range st.Chunks() {
			t.out <- c.Data
		}
		t.code, t.err = st.Wait()
		_ = in.Close() // a command that ended reads no more: a Write fails instead of blocking
		close(t.done)
	}()
	return t, nil
}

var _ runtime.TerminalAdapter = (*Fake)(nil)

// TerminalCall is one terminal the fake was asked to open.
type TerminalCall struct {
	Env string
	Req runtime.TerminalRequest
}

// Terminals returns every terminal the fake was asked to open, in order.
func (f *Fake) Terminals() []TerminalCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TerminalCall(nil), f.terminals...)
}

// Sizes returns the sizes a terminal of the environment was resized to, in order.
func (f *Fake) Sizes(id string) [][2]uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]uint16(nil), f.sizes[id]...)
}

type fakeTerminal struct {
	f      *Fake
	id     string
	in     *io.PipeWriter
	st     runtime.ExecStream
	cancel context.CancelFunc
	out    chan []byte
	done   chan struct{}
	code   int
	err    error

	mu   sync.Mutex
	rest []byte
}

func (t *fakeTerminal) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for len(t.rest) == 0 {
		b, ok := <-t.out
		if !ok {
			return 0, io.EOF
		}
		t.rest = b
	}
	n := copy(p, t.rest)
	t.rest = t.rest[n:]
	return n, nil
}

func (t *fakeTerminal) Write(p []byte) (int, error) { return t.in.Write(p) }

func (t *fakeTerminal) Resize(cols, rows uint16) error {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	if t.f.sizes == nil {
		t.f.sizes = map[string][][2]uint16{}
	}
	t.f.sizes[t.id] = append(t.f.sizes[t.id], [2]uint16{cols, rows})
	return nil
}

func (t *fakeTerminal) Close() error {
	_ = t.in.Close()
	t.cancel()
	return nil
}

func (t *fakeTerminal) Wait() (int, error) {
	<-t.done
	if t.code == 130 {
		return 130, nil // ended by Close
	}
	return t.code, nil
}
