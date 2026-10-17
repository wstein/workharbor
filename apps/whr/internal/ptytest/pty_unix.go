// SPDX-License-Identifier: EUPL-1.2

//go:build linux || darwin

package ptytest

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func platformSupported() bool { return true }

func ioctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

type winsize struct{ rows, cols, x, y uint16 }

func setWinsize(f *os.File, rows, cols int) error {
	ws := winsize{rows: uint16(rows), cols: uint16(cols)}
	return ioctl(f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

// WindowSize reports the terminal size of fd.
func WindowSize(fd uintptr) (rows, cols int, err error) {
	var ws winsize
	if err := ioctl(fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil {
		return 0, 0, err
	}
	return int(ws.rows), int(ws.cols), nil
}

// MakeRaw puts the terminal on fd into a mode for key-by-key input: no echo,
// no line editing, no CR translation, no flow control. Signal generation
// (Ctrl-C) and output post-processing stay on so a program under test still
// receives SIGINT and writes plain "\n". The returned function restores the
// previous settings.
func MakeRaw(fd uintptr) (restore func(), err error) {
	var old syscall.Termios
	if err := ioctl(fd, tcGet, uintptr(unsafe.Pointer(&old))); err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN
	raw.Iflag &^= syscall.ICRNL | syscall.IXON
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, tcSet, uintptr(unsafe.Pointer(&raw))); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, tcSet, uintptr(unsafe.Pointer(&old))) }, nil
}

func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	name, err := prepareMaster(master)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	slave, err = os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("open %s: %w", name, err)
	}
	return master, slave, nil
}

// startProcess runs cmd as a session leader whose controlling terminal and
// standard streams are slave.
func startProcess(cmd *exec.Cmd, slave *os.File) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	return cmd.Start()
}

// killGroup kills every process in the group led by pid.
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// ProcessGone reports whether pid no longer runs; a zombie that only awaits
// reaping by an init process counts as gone.
func ProcessGone(pid int) bool {
	if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
		return true
	}
	var state string
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		s := string(b)
		if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
			state = s[i+2 : i+3]
		}
	} else if out, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output(); err == nil {
		state = strings.TrimSpace(string(out))
		if state == "" {
			return true
		}
		state = state[:1]
	}
	return state == "Z"
}
