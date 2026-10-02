//go:build darwin

package apple

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY makes a pseudo-terminal: the master the supervisor holds and the slave
// the container CLI gets as its terminal. On macOS the slave is named by an ioctl
// on the master after it is granted and unlocked.
func openPTY() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	fd := int(m.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("grant the pty: %w", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("unlock the pty: %w", err)
	}
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 { //nolint:gosec,staticcheck // x/sys has no exported pointer ioctl for TIOCPTYGNAME on macOS (SYS_IOCTL is deprecated but works through libSystem; TestTerminalLive proves it); the kernel writes the slave's name into this buffer
		_ = m.Close()
		return nil, nil, fmt.Errorf("name the pty: %w", errno)
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	s, err := os.OpenFile(string(name[:n]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("open the pty's slave: %w", err)
	}
	return m, s, nil
}
