//go:build darwin

package setup

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// openPty opens a pseudo terminal (the macOS way): the master end to type
// into, the slave end for a Terminal to read from.
func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	fd := m.Fd()
	for _, req := range []uintptr{syscall.TIOCPTYGRANT, syscall.TIOCPTYUNLK} {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, 0); e != 0 {
			t.Skipf("pseudo terminal ioctl: %v", e)
		}
	}
	var name [128]byte
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); e != 0 { //nolint:gosec // the ioctl fills a buffer
		t.Skipf("pseudo terminal name: %v", e)
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	s, err := os.OpenFile(string(name[:n]), os.O_RDWR, 0)
	if err != nil {
		t.Skipf("pseudo terminal slave: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return m, s
}
