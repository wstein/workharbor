// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package wizard

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

// IsTerminal verifies a terminal ioctl, rather than accepting arbitrary character devices.
func IsTerminal(f *os.File) bool {
	_, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	return err == nil
}

type terminalReader struct {
	ctx  context.Context
	file *os.File
}

// Reader waits in bounded intervals so SIGINT/SIGTERM can cancel a blocked prompt
// without a background reader, changing terminal flags or closing the caller's fd.
func Reader(ctx context.Context, file *os.File) io.Reader { return terminalReader{ctx, file} }
func (r terminalReader) Read(b []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		fds := []unix.PollFd{{Fd: int32(r.file.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 50)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		n, err = unix.Read(int(r.file.Fd()), b)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if n == 0 && err == nil {
			return 0, io.EOF
		}
		return n, err
	}
}
