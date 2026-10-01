package runtimetest

import (
	"context"
	"strconv"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Exec implements runtime.Adapter. The fake understands a few commands:
//
//	echo WORDS...   writes them to stdout, exit 0
//	err WORDS...    writes them to stderr, exit 0
//	exit N          exit code N
//	sleep           blocks until the context is cancelled
//
// Anything else exits 127.
func (f *Fake) Exec(ctx context.Context, id string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	f.mu.Lock()
	e, err := f.own(id)
	if err != nil {
		f.mu.Unlock()
		return nil, err
	}
	if e.state != domain.EnvRunning {
		f.mu.Unlock()
		return nil, runtime.ErrNotRunning
	}
	f.mu.Unlock()

	st := &fakeStream{chunks: make(chan runtime.Chunk, 8), done: make(chan struct{})}
	go func() {
		defer close(st.done)
		defer close(st.chunks)
		send := func(s runtime.Stream, text string) {
			select {
			case st.chunks <- runtime.Chunk{Stream: s, Data: []byte(text)}:
			case <-ctx.Done():
			}
		}
		if len(req.Cmd) == 0 {
			st.code = 127
			return
		}
		f.logLine(id, strings.Join(req.Cmd, " "))
		switch req.Cmd[0] {
		case "echo":
			send(runtime.Stdout, strings.Join(req.Cmd[1:], " ")+"\n")
		case "err":
			send(runtime.Stderr, strings.Join(req.Cmd[1:], " ")+"\n")
		case "exit":
			if len(req.Cmd) > 1 {
				st.code, _ = strconv.Atoi(req.Cmd[1])
			}
		case "sleep":
			<-ctx.Done()
			st.err = ctx.Err()
			st.code = 130
		default:
			st.code = 127
		}
	}()
	return st, nil
}

type fakeStream struct {
	chunks chan runtime.Chunk
	done   chan struct{}
	code   int
	err    error
}

func (s *fakeStream) Chunks() <-chan runtime.Chunk { return s.chunks }

func (s *fakeStream) Wait() (int, error) {
	<-s.done
	return s.code, s.err
}
