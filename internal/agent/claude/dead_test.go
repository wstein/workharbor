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
