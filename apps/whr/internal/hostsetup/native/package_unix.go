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
	Start() error
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
	if err = child.Start(); err != nil {
		return nil, err
	}
	process := &packageProcess{child: child, done: make(chan pkgmgr.Completion, 1)}
	go process.observe()
	return process, nil
}

type packageProcess struct {
	mu       sync.Mutex
	child    packageChild
	done     chan pkgmgr.Completion
	finished bool
}

func (p *packageProcess) Done() <-chan pkgmgr.Completion { return p.done }
func (p *packageProcess) Interrupt() error               { return p.signal(int(syscall.SIGINT)) }
func (p *packageProcess) Kill() error                    { return p.signal(int(syscall.SIGKILL)) }
func (p *packageProcess) signal(sig int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return nil
	}
	return p.child.Signal(sig)
}
func (p *packageProcess) observe() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		p.mu.Lock()
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
			if err != nil {
				completion.Err = err
			} else if signalErr != nil {
				completion.Err = signalErr
			}
			p.mu.Unlock()
			p.done <- completion
			close(p.done)
			return
		}
		p.mu.Unlock()
		<-ticker.C
	}
}

type nativePackageChild struct{ cmd *exec.Cmd }

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
func (c *nativePackageChild) Start() error          { return c.cmd.Start() }
func (c *nativePackageChild) Exited() (bool, error) { return childExited(c.cmd.Process.Pid) }
func (c *nativePackageChild) Signal(sig int) error {
	err := syscall.Kill(-c.cmd.Process.Pid, syscall.Signal(sig))
	if errors.Is(err, syscall.ESRCH) {
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
