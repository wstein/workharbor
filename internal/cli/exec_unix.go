//go:build unix

package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
)

var termName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,39}$`)

// runShellChild gives only fds 0..2 to the runtime; it never reads, writes or
// raw-modes the terminal. Notify discards job-control signals in the parent
// while leaving default dispositions in the exec'd child (Ignore would also
// make the child ignore them). The runtime stays in the terminal's process group.
func runShellChild(ctx context.Context, bin string, argv, env []string) (int, error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
	defer signal.Stop(signals)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Go creates descriptors close-on-exec. Also seal any descriptor inherited
	// from the program that launched whr, including a deliberately non-CLOEXEC fd.
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return 0, fmt.Errorf("list inherited descriptors: %w", err)
	}
	syscall.ForkLock.Lock()
	for _, entry := range entries {
		if fd, err := strconv.Atoi(entry.Name()); err == nil && fd > 2 {
			for {
				_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, syscall.FD_CLOEXEC)
				if errno == syscall.EINTR {
					continue
				}
				if errno != 0 && errno != syscall.EBADF { // the directory's own fd may already be closed
					syscall.ForkLock.Unlock()
					return 0, fmt.Errorf("seal inherited descriptor %d: %w", fd, errno)
				}
				break
			}
		}
	}
	syscall.ForkLock.Unlock()
	child, err := os.StartProcess(bin, argv, &os.ProcAttr{Env: env, Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}}) //nolint:gosec // runtime CLI and checked arguments
	if err != nil {
		return 0, err
	}
	type result struct {
		state *os.ProcessState
		err   error
	}
	done := make(chan result, 1)
	go func() { state, err := child.Wait(); done <- result{state, err} }()
	var res result
	for {
		select {
		case <-signals: // terminal signals belong to the child, never the parent
		case <-ctx.Done():
			_ = child.Kill()
			<-done
			return 0, ctx.Err()
		case res = <-done:
			if res.err != nil {
				return 0, res.err
			}
			if status, ok := res.state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			return res.state.ExitCode(), nil
		}
	}
}
