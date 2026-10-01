package claude

import (
	"context"

	"github.com/wstein/workharbor/internal/runtime"
)

// deadRunner starts a process that prints to stderr and exits 2 at once.
type deadRunner struct{}

func (deadRunner) Exec(_ context.Context, _ string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if st, ok := preflight(req, ""); ok {
		return st, nil
	}
	st := &stubStream{chunks: make(chan runtime.Chunk, 2), done: make(chan struct{}), code: 2}
	go func() {
		defer close(st.done)
		defer close(st.chunks)
		st.chunks <- runtime.Chunk{Stream: runtime.Stderr, Data: []byte("boom: cannot start\n")}
	}()
	return st, nil
}

// scripted starts a process that writes fixed stdout and stderr and then, if
// linger is set, stays alive until its context is cancelled.
type scripted struct {
	out, err []string
	linger   bool
}

func (r scripted) Exec(ctx context.Context, _ string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if st, ok := preflight(req, ""); ok {
		return st, nil
	}
	st := &stubStream{chunks: make(chan runtime.Chunk, 8), done: make(chan struct{})}
	go func() {
		defer close(st.done)
		defer close(st.chunks)
		send := func(s runtime.Stream, text string) {
			select {
			case st.chunks <- runtime.Chunk{Stream: s, Data: []byte(text)}:
			case <-ctx.Done():
			}
		}
		for _, l := range r.out {
			send(runtime.Stdout, l)
		}
		for _, l := range r.err {
			send(runtime.Stderr, l)
		}
		if r.linger {
			<-ctx.Done()
			st.code, st.err = 130, ctx.Err()
		}
	}()
	return st, nil
}

// preflight answers the adapter's instruction-file check (D38) with found, the
// paths it reports, when req is that check.
func preflight(req runtime.ExecRequest, found string) (runtime.ExecStream, bool) {
	if len(req.Cmd) < 4 || req.Cmd[3] != "whr-instruction-check" {
		return nil, false
	}
	st := &stubStream{chunks: make(chan runtime.Chunk, 1), done: make(chan struct{})}
	if found != "" {
		st.chunks <- runtime.Chunk{Stream: runtime.Stdout, Data: []byte(found)}
	}
	close(st.chunks)
	close(st.done)
	return st, true
}

// foundRunner reports a CLAUDE.md-family file to the preflight check.
type foundRunner struct{ found string }

func (r foundRunner) Exec(_ context.Context, _ string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if st, ok := preflight(req, r.found); ok {
		return st, nil
	}
	return deadRunner{}.Exec(context.Background(), "", req)
}
