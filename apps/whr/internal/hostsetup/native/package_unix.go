// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package native

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
)

type packageChild interface {
	Start(context.Context) error
	Exited() (bool, error)
	Signal(int) error
	Wait() pkgmgr.Completion
	Release() error
}

// PackageExec implements the mutating fixed Homebrew command boundary separately
// from Host's read-only detector. Its zero value is usable and starts no process
// until Start. It supplies neither confirmation nor a journal: the package engine
// owns those gates. Executables at the two canonical Homebrew paths are trusted
// host installations; this adapter does not sandbox their behavior.
type PackageExec struct {
	launch func(pkgmgr.Command) (packageChild, error)
}

var _ pkgmgr.Exec = PackageExec{}

func (p PackageExec) Start(ctx context.Context, c pkgmgr.Command) (pkgmgr.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := pkgmgr.ValidateBrewCommand(c); err != nil {
		return nil, err
	}
	// Never retain caller-owned argv/environment after crossing the boundary.
	copyCommand := c
	copyCommand.Argv = append([]string(nil), c.Argv...)
	copyCommand.Env = map[string]string{}
	for k, v := range c.Env {
		copyCommand.Env[k] = v
	}
	launch := p.launch
	if launch == nil {
		launch = newPackageChild
	}
	child, err := launch(copyCommand)
	if err != nil {
		return nil, err
	}
	if child == nil {
		return nil, errors.New("package child unavailable")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = child.Start(ctx); err != nil {
		return nil, err
	}
	process := &packageProcess{child: child, done: make(chan pkgmgr.Completion, 1), reaped: make(chan struct{}), observerDone: make(chan struct{})}
	go process.observe()
	// A cooperative launcher can return success concurrently with cancellation.
	// Retain ownership and complete cleanup before reporting startup cancellation.
	if err = ctx.Err(); err != nil {
		if cleanupErr := process.Kill(); cleanupErr != nil {
			return process, errors.Join(err, cleanupErr)
		}
		return nil, err
	}
	return process, nil
}

type packageProcess struct {
	mu           sync.Mutex
	operations   sync.Mutex
	child        packageChild
	done         chan pkgmgr.Completion
	reaped       chan struct{}
	finished     bool
	paused       bool
	observerDone chan struct{}
	cleanupErr   error
}

func (p *packageProcess) Done() <-chan pkgmgr.Completion { return p.done }
func (p *packageProcess) Interrupt() error               { return p.signal(int(syscall.SIGINT)) }
func (p *packageProcess) Kill() error {
	p.operations.Lock()
	defer p.operations.Unlock()
	if err := p.signalOwned(int(syscall.SIGKILL)); err != nil {
		return err
	}
	<-p.reaped
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cleanupErr
}
func (p *packageProcess) signal(sig int) error {
	p.operations.Lock()
	defer p.operations.Unlock()
	return p.signalOwned(sig)
}

// operations serializes pause/join/resume while mu protects unreaped identity.
// A failed signal leaves an owned handle, but no idle polling goroutine. A later
// explicit retry can resume observation; a terminal error never signals again.
func (p *packageProcess) signalOwned(sig int) error {
	p.mu.Lock()
	if p.finished {
		err := p.cleanupErr
		p.mu.Unlock()
		return err
	}
	err := p.child.Signal(sig)
	if err != nil {
		p.paused = true
		done := p.observerDone
		p.mu.Unlock()
		<-done
		return pkgmgr.NewCleanupError(p, err)
	}
	if p.paused {
		p.paused = false
		p.observerDone = make(chan struct{})
		go p.observe()
	}
	p.mu.Unlock()
	return nil
}
func (p *packageProcess) observe() {
	done := p.observerDone
	defer close(done)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		p.mu.Lock()
		if p.paused {
			p.mu.Unlock()
			return
		}
		exited, err := p.child.Exited()
		if exited || err != nil {
			// Keep PID ownership through the final group signal, then reap. Serializing
			// engine signals with this section prevents signals to an already-reused PID.
			var signalErr error
			if errors.Is(err, errOwnershipLost) {
				_ = p.child.Release()
			} else {
				signalErr = p.child.Signal(int(syscall.SIGKILL))
			}
			completion := p.child.Wait()
			p.finished = true
			if cleanupCause := errors.Join(err, signalErr); cleanupCause != nil {
				p.cleanupErr = pkgmgr.NewCleanupError(p, cleanupCause)
				completion.Err = errors.Join(completion.Err, p.cleanupErr)
			}
			close(p.reaped)
			p.mu.Unlock()
			p.done <- completion
			close(p.done)
			return
		}
		p.mu.Unlock()
		<-ticker.C
	}
}

type nativePackageChild struct {
	cmd    *exec.Cmd
	exited bool
}

func newPackageChild(c pkgmgr.Command) (packageChild, error) {
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+c.Env[k])
	}
	cmd.Dir = "/"
	cmd.Stdin = nil
	// No raw process output is retained, logged or returned. Discard drains both
	// streams without allocating an unbounded tail or accepting credential input.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 100 * time.Millisecond
	return &nativePackageChild{cmd: cmd}, nil
}

// Start has no detached startup goroutine. Go's synchronous Cmd.Start cannot
// interrupt an in-flight kernel process-creation operation; context cancellation
// is checked before it and the adapter cleans up success racing cancellation
// before returning. Once successful startup is returned, only the engine owns
// interrupt/grace/kill. This is not an arbitrary kernel-stall deadline guarantee.
func (c *nativePackageChild) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.cmd.Start()
}
func (c *nativePackageChild) Exited() (bool, error) {
	exited, err := childExited(c.cmd.Process.Pid)
	if err == nil && exited {
		c.exited = true
	}
	return exited, err
}
func (c *nativePackageChild) Signal(sig int) error {
	return c.groupSignalResult(syscall.Kill(-c.cmd.Process.Pid, syscall.Signal(sig)))
}

// groupSignalResult applies the native probe's group-signal policy: a missing
// group is gone, and where the OS refuses (EPERM) to signal the group of an
// exited, unreaped leader that refusal is not a failure. A live child still
// surfaces every error. Signal runs under the process mutex, as does Exited.
func (c *nativePackageChild) groupSignalResult(err error) error {
	if errors.Is(err, syscall.ESRCH) || signalOfExitedLeaderIgnored(err, c.exited, exitedLeaderSignalEPERM) {
		return nil
	}
	return err
}
func (c *nativePackageChild) Release() error { return c.cmd.Process.Release() }
func (c *nativePackageChild) Wait() pkgmgr.Completion {
	err := c.cmd.Wait()
	exit := -1
	if c.cmd.ProcessState != nil {
		exit = c.cmd.ProcessState.ExitCode()
	}
	return pkgmgr.Completion{Exit: exit, Err: err}
}
