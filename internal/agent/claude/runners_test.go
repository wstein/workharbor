package claude

import (
	"context"

	"github.com/wstein/workharbor/internal/runtime"
)

// deadRunner starts a process that prints to stderr and exits 2 at once.
type deadRunner struct{}

func (deadRunner) Exec(context.Context, string, runtime.ExecRequest) (runtime.ExecStream, error) {
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

func (r scripted) Exec(ctx context.Context, _ string, _ runtime.ExecRequest) (runtime.ExecStream, error) {
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
