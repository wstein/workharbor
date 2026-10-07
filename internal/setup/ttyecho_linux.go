package setup

import "golang.org/x/sys/unix"

// echoOff switches terminal echo off on fd and returns what restores it. A
// failure (fd is not a terminal) returns a no-op restore.
func echoOff(fd int) func() {
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return func() {}
	}
	old := *t
	t.Lflag &^= unix.ECHO
	if unix.IoctlSetTermios(fd, unix.TCSETS, t) != nil {
		return func() {}
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, &old) }
}
