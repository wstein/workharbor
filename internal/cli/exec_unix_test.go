//go:build unix

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestShellChildInheritsOnlyTheTerminalDescriptors(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "descriptor")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	// Make a foreign, deliberately non-close-on-exec descriptor above those the
	// test harness uses. No terminal is captured and no terminal byte is read.
	fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_DUPFD, 100)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer func() { _ = syscall.Close(int(fd)) }()
	code, err := runShellChild(t.Context(), "/bin/sh", []string{"sh", "-c", fmt.Sprintf("if [ -e /dev/fd/%d ]; then exit 97; fi", fd)}, []string{"PATH=/usr/bin:/bin"})
	if err != nil || code != 0 {
		t.Fatalf("foreign descriptor leaked: exit %d, error %v", code, err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
	if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("foreign descriptor not close-on-exec: flags %d, error %v", flags, errno)
	}
}

func TestShellChildReturnsItsExitAndSignalStatus(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		want         int
	}{
		{"exit", "exit 21", 21},
		{"child signal", "kill -INT $$", 128 + int(syscall.SIGINT)},
		{"parent interrupt", "kill -INT $PPID; exit 22", 22},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := runShellChild(t.Context(), "/bin/sh", []string{"sh", "-c", tc.script}, []string{"PATH=/usr/bin:/bin"})
			if err != nil || code != tc.want {
				t.Fatalf("exit %d, error %v, want %d", code, err, tc.want)
			}
		})
	}
}

func TestShellChildIsKilledAndWaitedWhenHoldContextEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runShellChild(ctx, "/bin/sh", []string{"sh", "-c", "exec /bin/sleep 30"}, []string{"PATH=/usr/bin:/bin"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Fatalf("child not ended promptly: %v after %s", err, time.Since(started))
	}
}
