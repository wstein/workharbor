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
//	cat             copies stdin to stdout until stdin ends
//	alive           exit 0 while a sleep runs in the environment, 1 when none does
//	sh -c ... ls-files ...   the number of tracked files, TrackedFiles, as the supervisor counts them
//	mkdir ...       exit 0 (the command is only logged; the fake has no files)
//	git ...         exit GitExit (the command is only logged; the fake has no repositories)
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
	f.execs = append(f.execs, ExecCall{Env: id, Req: req})
	sleeping := len(req.Cmd) > 0 && req.Cmd[0] == "sleep"
	if sleeping {
		e.procs++ // the process exists before Exec returns
	}
	alive := e.procs > 0
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
		if f.OnExec != nil {
			if out, errText, code, ok := f.OnExec(id, req.Cmd); ok {
				for len(out) > 0 {
					n := min(len(out), 32<<10)
					send(runtime.Stdout, string(out[:n]))
					out = out[n:]
				}
				if errText != "" {
					send(runtime.Stderr, errText)
				}
				st.code = code
				return
			}
		}
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
			if !f.Defects.CancelLeavesRun {
				f.mu.Lock()
				e.procs--
				f.mu.Unlock()
			}
			st.err = ctx.Err()
			st.code = 130
		case "git":
			// logged above; a service test reads the log to see what was asked
			st.code = f.GitExit
		case "sh":
			// The supervisor's count of a checkout's tracked files is the one `sh -c`
			// the fake answers; every other shell command is unknown.
			if len(req.Cmd) > 2 && strings.Contains(req.Cmd[2], "ls-files") {
				f.mu.Lock()
				n := f.TrackedFiles
				f.mu.Unlock()
				send(runtime.Stdout, strconv.Itoa(n)+"\n")
			} else {
				st.code = 127
			}
		case "mkdir":
			// logged above; the fake has no file system
		case "alive":
			if !alive {
				st.code = 1
			}
		case "cat":
			f.cat(ctx, req, send)
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

// cat copies the request's stdin to stdout as it arrives. The reader is read
// in its own goroutine, so a cancelled context ends the command even while a
// read blocks.
func (f *Fake) cat(ctx context.Context, req runtime.ExecRequest, send func(runtime.Stream, string)) {
	if req.Stdin == nil || f.Defects.IgnoreStdin {
		return
	}
	data := make(chan []byte)
	go func() {
		defer close(data)
		buf := make([]byte, 4096)
		for {
			n, err := req.Stdin.Read(buf)
			if n > 0 {
				select {
				case data <- append([]byte(nil), buf[:n]...):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		select {
		case b, ok := <-data:
			if !ok {
				return
			}
			send(runtime.Stdout, string(b))
		case <-ctx.Done():
			return
		}
	}
}
