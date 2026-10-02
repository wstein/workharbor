//go:build unix

package apple

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Terminal implements runtime.TerminalAdapter: it runs `container exec -t -i`
// with a pseudo-terminal of its own as the client's terminal, so the guest's
// command has a real terminal, with signals and a size. The client is the
// container CLI, in its own session with the pty as controlling terminal; the
// supervisor holds the master. Ending it hangs the guest's terminal up.
func (a *Adapter) Terminal(ctx context.Context, id string, req runtime.TerminalRequest) (runtime.Terminal, error) {
	c, err := a.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.info(c).State != domain.EnvRunning {
		return nil, runtime.ErrNotRunning
	}
	if len(req.Cmd) == 0 {
		return nil, errors.New("a terminal needs a command")
	}
	args := []string{"exec", "-t", "-i"}
	if req.Dir != "" {
		args = append(args, "-w", req.Dir)
	}
	cleanup := func() {}
	if len(req.Env) > 0 {
		path, remove, err := envFile(req.Env)
		if err != nil {
			return nil, err
		}
		cleanup = remove
		args = append(args, "--env-file", path)
	}
	args = append(args, id)
	// The command runs through a small sh that records its own process ID, which
	// is also its session's ID (container exec starts it as a session leader): a
	// shell with job control puts a background job in a group of its own, so the
	// terminal's hangup and a signal to the shell's group do not reach it, but
	// every process of the session can be found by that ID (Close).
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		cleanup()
		return nil, err
	}
	sidfile := "/tmp/whr-term-" + hex.EncodeToString(b) + ".sid"
	args = append(args, "sh", "-c", `echo $$ > "$0"; exec "$@"`, sidfile)
	args = append(args, req.Cmd...)

	master, slave, err := openPTY()
	if err != nil {
		cleanup()
		return nil, err
	}
	cols, rows := req.Cols, req.Rows
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows}); err != nil {
		_ = master.Close()
		_ = slave.Close()
		cleanup()
		return nil, fmt.Errorf("set the terminal size: %w", err)
	}
	cmd := exec.Command(a.binary(), args...) //nolint:gosec,noctx // the container CLI with arguments built from checked values; the terminal outlives the request that opened it
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = slave.Close()
		cleanup()
		return nil, fmt.Errorf("start container exec: %w", err)
	}
	_ = slave.Close() // the child has its own copy
	t := &terminal{master: master, cmd: cmd, cleanup: cleanup, done: make(chan struct{})}
	t.killSession = func() {
		// Hang up the session's processes, then kill what is left of them, with
		// the guest's own tools (procps); an environment without pkill is left to
		// the terminal's hangup alone.
		kctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		script := `p=$(cat "$0" 2>/dev/null) || exit 0; case $p in ''|*[!0-9]*) exit 0;; esac; ` +
			`{ pkill -HUP -s "$p" && sleep 1 && pkill -KILL -s "$p"; } 2>/dev/null; rm -f "$0"`
		_, _, _ = a.run(kctx, nil, "exec", id, "sh", "-c", script, sidfile)
	}
	go func() {
		t.err = cmd.Wait()
		close(t.done)
	}()
	// container exec hands the guest its terminal size only after the command
	// has started, so a command that reads the size at once sees 0 by 0. A
	// change of size sends the guest a SIGWINCH and the size with it: nudge the
	// terminal once the command is up, by a column and back.
	go func() {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-t.done:
			return
		}
		// From the size the terminal has now: the client may have resized it already.
		if ws, err := unix.IoctlGetWinsize(int(t.master.Fd()), unix.TIOCGWINSZ); err == nil {
			_ = t.Resize(ws.Col+1, ws.Row)
			_ = t.Resize(ws.Col, ws.Row)
		}
	}()
	return t, nil
}

var _ runtime.TerminalAdapter = (*Adapter)(nil)

// terminal is a running `container exec -t -i` and the master of its pty.
type terminal struct {
	master  *os.File
	cmd     *exec.Cmd
	cleanup func()
	done    chan struct{}
	err     error // the result of cmd.Wait, valid after done is closed
	// killSession ends every process of the guest command's session.
	killSession func()

	closeOnce sync.Once
}

func (t *terminal) Read(p []byte) (int, error) {
	n, err := t.master.Read(p)
	if err != nil {
		// A pty master read fails with EIO once the slave is gone: the end.
		if errors.Is(err, syscall.EIO) {
			return n, io.EOF
		}
	}
	return n, err
}

func (t *terminal) Write(p []byte) (int, error) { return t.master.Write(p) }

func (t *terminal) Resize(cols, rows uint16) error {
	return unix.IoctlSetWinsize(int(t.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows})
}

// Close hangs the terminal up: the master is closed, which sends the client
// SIGHUP, and if it has not ended soon it is killed.
func (t *terminal) Close() error {
	t.closeOnce.Do(func() {
		if t.killSession != nil {
			t.killSession()
		}
		_ = t.master.Close()
		select {
		case <-t.done:
		case <-time.After(3 * time.Second):
			_ = t.cmd.Process.Signal(syscall.SIGKILL)
			<-t.done
		}
		t.cleanup()
	})
	return nil
}

// Wait returns the client's exit code, which is the guest command's.
func (t *terminal) Wait() (int, error) {
	<-t.done
	var ee *exec.ExitError
	switch {
	case t.err == nil:
		return 0, nil
	case errors.As(t.err, &ee):
		return ee.ExitCode(), nil
	}
	return -1, t.err
}
