package apple

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortStrings(s []string) { sort.Strings(s) }

// ErrBadEnv is returned for an environment entry that is not KEY=VALUE with a
// plain key and a single-line value. The message names the key, never the value.
var ErrBadEnv = errors.New("apple: bad environment entry")

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envPipe carries the entries to `container exec --env-file` through a pipe, so no
// value is on a command line (where `ps` and an ExitError would show it) and no
// file holds it either: the file this replaced stayed in the temp directory for
// the whole life of the exec, hours for an agent, and a crash left it there. The
// read end is handed to the child as its descriptor 3 (cmd.ExtraFiles) and the CLI
// is told to read /dev/fd/3 (checked with container 1.5.0: it reads a pipe there).
// An entry without "=" is refused: for the CLI it means inheriting whr's own
// variable. The CLI takes each value verbatim up to the end of the line (checked
// with container 1.5.0), so a value may not span lines.
type envPipe struct {
	data []byte
	pr   *os.File
	pw   *os.File
}

// envFD is the descriptor the child gets: ExtraFiles[0] is 3.
const envFD = "/dev/fd/3"

func newEnvPipe(env []string) (*envPipe, error) {
	var b strings.Builder
	for i, e := range env {
		k, v, ok := strings.Cut(e, "=")
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: entry %d has no '=', which would copy a variable from whr's own environment", ErrBadEnv, i)
		case !envKey.MatchString(k):
			return nil, fmt.Errorf("%w: entry %d has a key that is not a plain name", ErrBadEnv, i)
		case strings.ContainsAny(v, "\n\r\x00"):
			return nil, fmt.Errorf("%w: the value of %s spans lines or holds NUL", ErrBadEnv, k)
		}
		b.WriteString(e)
		b.WriteByte('\n')
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &envPipe{data: []byte(b.String()), pr: pr, pw: pw}, nil
}

// attach makes the read end the child's descriptor 3.
func (e *envPipe) attach(cmd *exec.Cmd) { cmd.ExtraFiles = []*os.File{e.pr} }

// started is called once the child exists: the parent's copy of the read end is
// closed, and the entries are written from a goroutine, so a large set cannot
// block on the pipe's buffer. The goroutine ends when the child has read them or
// when close is called (a write to a pipe nobody reads fails).
func (e *envPipe) started() {
	_ = e.pr.Close()
	go func() {
		_, _ = e.pw.Write(e.data)
		_ = e.pw.Close()
	}()
}

// close releases both ends; it is safe to call twice.
func (e *envPipe) close() {
	_ = e.pr.Close()
	_ = e.pw.Close()
}

// SweepEnvFiles removes the env files an earlier whr left in dir (the temp
// directory by default): `whr-env-*`, regular, mode 0600, owned by this user and
// last written before before, which is when this process started. A file of another
// name, owner or mode is left alone. It returns what it removed. The files were the
// way the environment reached `container exec` before it went through a pipe; a
// crash could leave one with an agent's API key in it.
func SweepEnvFiles(dir string, before time.Time) []string {
	if dir == "" {
		dir = os.TempDir()
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "whr-env-") || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode().Perm() != 0o600 || !info.ModTime().Before(before) {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() { //nolint:gosec // a uid fits an int
			continue
		}
		path := filepath.Join(dir, e.Name())
		if os.Remove(path) == nil {
			removed = append(removed, path)
		}
	}
	return removed
}

// killGrace is how long whr-shim waits after SIGINT before SIGKILL.
const killGrace = 2 * time.Second

type stream struct {
	chunks chan runtime.Chunk
	done   chan struct{}
	code   int
	err    error
}

func (s *stream) Chunks() <-chan runtime.Chunk { return s.chunks }

func (s *stream) Wait() (int, error) {
	<-s.done
	return s.code, s.err
}

// Exec implements runtime.Adapter. Output is streamed as it is produced; stdin
// is copied into the command and closed when it ends. With a shim the command
// runs under whr-shim, and cancelling the context signals its process group
// inside the guest, because SIGINT to the `container exec` client is not
// forwarded (spike #2): the guest process would keep running.
func (a *Adapter) Exec(ctx context.Context, id string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	c, err := a.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.info(c).State != domain.EnvRunning {
		return nil, runtime.ErrNotRunning
	}

	args := []string{"exec"}
	if req.Stdin != nil {
		args = append(args, "-i")
	}
	if req.Dir != "" {
		args = append(args, "-w", req.Dir)
	}
	cleanup := func() {}
	var envp *envPipe
	if len(req.Env) > 0 {
		var err error
		if envp, err = newEnvPipe(req.Env); err != nil {
			return nil, err
		}
		cleanup = envp.close
		args = append(args, "--env-file", envFD)
	}
	args = append(args, id)
	pidfile := ""
	if a.shim != "" {
		b := make([]byte, 6)
		if _, err := rand.Read(b); err != nil {
			cleanup()
			return nil, err
		}
		pidfile = "/tmp/whr-exec-" + hex.EncodeToString(b) + ".pid"
		args = append(args, a.shim, "run", "-pidfile", pidfile, "--")
	}
	args = append(args, req.Cmd...)

	bin := a.binary()
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx)) // the client is ended by us, after the guest process
	cmd := exec.CommandContext(runCtx, bin, args...)               //nolint:gosec // the container CLI with arguments built from checked values
	// The client's stdin is a pipe of our own, fed from the caller's reader by a
	// goroutine of ours. Handing the caller's reader to exec would make Wait
	// wait for exec's copy goroutine, which never ends while the caller keeps
	// its pipe open (an agent's stream-json input): a cancelled exec would
	// never finish and the supervisor could not shut down with a session
	// attached (found by the serve integration run). With our pipe, ending the
	// client closes it and Wait does not depend on the caller.
	var stdinW *os.File
	if req.Stdin != nil {
		pr, pw, perr := os.Pipe()
		if perr != nil {
			stop()
			cleanup()
			return nil, perr
		}
		cmd.Stdin, stdinW = pr, pw
		defer func() { _ = pr.Close() }() // the child has its own copy once started
		go func() {
			_, _ = io.Copy(pw, req.Stdin) // ends when the caller's reader ends or the client is gone
			_ = pw.Close()
		}()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stop()
		cleanup()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stop()
		cleanup()
		return nil, err
	}
	if envp != nil {
		envp.attach(cmd)
	}
	if err := cmd.Start(); err != nil {
		stop()
		cleanup()
		return nil, err
	}
	if envp != nil {
		envp.started()
	}

	st := &stream{chunks: make(chan runtime.Chunk, 16), done: make(chan struct{})}
	var pumps sync.WaitGroup
	pump := func(r io.Reader, which runtime.Stream) {
		defer pumps.Done()
		br := bufio.NewReaderSize(r, 32<<10)
		buf := make([]byte, 32<<10)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				data := append([]byte(nil), buf[:n]...)
				select {
				case st.chunks <- runtime.Chunk{Stream: which, Data: data}:
				case <-runCtx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	pumps.Add(2)
	go pump(stdout, runtime.Stdout)
	go pump(stderr, runtime.Stderr)

	waited := make(chan error, 1)
	go func() {
		pumps.Wait()
		err := cmd.Wait()
		if stdinW != nil {
			_ = stdinW.Close() // a copy blocked on a write to a client that is gone ends
		}
		cleanup()
		waited <- err
	}()
	go func() {
		defer close(st.done)
		defer close(st.chunks)
		defer stop()
		select {
		case err := <-waited:
			st.code = exitCode(err)
		case <-ctx.Done():
			// Signal the process group in the guest first, then end the client.
			if pidfile != "" {
				kctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*killGrace)
				_, _, _ = a.run(kctx, nil, "exec", id, a.shim, "kill", "-pidfile", pidfile, "-grace", killGrace.String())
				cancel()
			}
			stop()
			<-waited
			st.code, st.err = 130, ctx.Err()
		}
	}()
	return st, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint // a direct type check on exec's own error
		return ee.ExitCode()
	}
	return 1
}

// binary returns the container CLI used by Exec: the adapter's runner is a
// closure, so the path is looked up again.
func (a *Adapter) binary() string {
	if p, err := exec.LookPath("container"); err == nil {
		return p
	}
	return "container"
}
