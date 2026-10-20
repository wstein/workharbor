// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

// Package native supplies read-only Unix ports for the host setup detector.
// The caller supplies a trusted filesystem root; Open does not run detection.
// Execution is solely for curated version probes, not arbitrary untrusted argv.
package native

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"golang.org/x/sys/unix"
)

const outputLimit = 64 * 1024

var errUnsafe = errors.New("unsafe version probe")
var errOutputLimit = errors.New("version probe output limit exceeded")

// Host owns a read-only root directory descriptor. Paths supplied to its ports
// are absolute virtual paths within that root. Close may race with port calls;
// those already holding duplicated descriptors complete against the pinned root.
type Host struct {
	mu   sync.Mutex
	root *os.File
}

func Open(root string) (*Host, error) {
	if !path.IsAbs(root) {
		return nil, errUnsafe
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return &Host{root: os.NewFile(uintptr(fd), root)}, nil
}
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.root == nil {
		return nil
	}
	err := h.root.Close()
	h.root = nil
	return err
}
func (h *Host) rootFD() (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.root == nil {
		return -1, os.ErrClosed
	}
	// F_DUPFD_CLOEXEC avoids a descriptor inheritance window in other goroutines.
	return unix.FcntlInt(h.root.Fd(), unix.F_DUPFD_CLOEXEC, 0)
}
func canonical(p string) bool {
	return path.IsAbs(p) && path.Clean(p) == p && !strings.ContainsRune(p, 0)
}

// parent pins every component independently. It never follows even an internal
// symlink; the detector's explicit resolver handles links before requesting the
// canonical target. Descriptor-relative operations remain safe if names move.
func (h *Host) parent(p string) (int, string, error) {
	if !canonical(p) {
		return -1, "", errUnsafe
	}
	fd, err := h.rootFD()
	if err != nil {
		return -1, "", err
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, part := range parts[:len(parts)-1] {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return -1, "", e
		}
		fd = next
	}
	leaf := parts[len(parts)-1]
	if leaf == "" {
		leaf = "."
	}
	return fd, leaf, nil
}
func (h *Host) Lstat(p string) (detect.File, error) {
	fd, leaf, err := h.parent(p)
	if err != nil {
		return detect.File{}, err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstatat(fd, leaf, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return detect.File{}, err
	}
	kind := "special"
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		kind = "regular"
	case unix.S_IFDIR:
		kind = "directory"
	case unix.S_IFLNK:
		kind = "symlink"
	}
	return detect.File{Kind: kind, Executable: st.Mode&0111 != 0}, nil
}
func (h *Host) Readlink(p string) (string, error) {
	fd, leaf, err := h.parent(p)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	// Catalogue/path limits are smaller, but retain the complete OS link target
	// or refuse; never resolve a silently truncated link.
	b := make([]byte, 64*1024)
	n, err := unix.Readlinkat(fd, leaf, b)
	if err != nil {
		return "", err
	}
	if n == len(b) {
		return "", errUnsafe
	}
	return string(b[:n]), nil
}
func (h *Host) executable(p string) (*os.File, error) {
	if p == "/usr/bin/git" || path.Base(p) == "brew" {
		return nil, errUnsafe
	}
	parent, leaf, err := h.parent(p)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), p)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
		f.Close()
		return nil, errUnsafe
	}
	return f, nil
}

type boundedOutput struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	available := outputLimit - w.buf.Len()
	if len(p) > available {
		w.buf.Write(p[:available])
		w.overflow = true
		w.cancel()
		return available, errOutputLimit
	}
	return w.buf.Write(p)
}
func (w *boundedOutput) result() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String(), w.overflow
}

// Run uses an opened executable descriptor, separate argv and an exact clean
// environment. No shell, inherited stdin, stderr or private environment reaches
// the result. The trusted catalogue still owns version-argument safety: process
// spawning alone cannot prove that arbitrary programs never access a network.
func (h *Host) Run(ctx context.Context, p detect.Probe) (detect.Output, error) {
	if ctx.Err() != nil {
		return detect.Output{}, ctx.Err()
	}
	if p.Timeout <= 0 || p.Timeout > 5*time.Second || len(p.Env) != 2 || p.Env["PATH"] != detect.FixedPATH || p.Env["LC_ALL"] != "C" || len(p.Args) == 0 || len(p.Args) > 4 {
		return detect.Output{}, errUnsafe
	}
	for _, arg := range p.Args {
		if len(arg) == 0 || len(arg) > 256 || strings.ContainsRune(arg, 0) {
			return detect.Output{}, errUnsafe
		}
	}
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	f, err := h.executable(p.Executable)
	if err != nil {
		return detect.Output{}, err
	}
	defer f.Close()
	if ctx.Err() != nil {
		return detect.Output{}, ctx.Err()
	}
	// ExtraFiles[0] is descriptor3 on both supported Unix platforms. /dev/fd is
	// supplied by the OS, not PATH; the opened file remains pinned through exec.
	cmd := exec.Command("/dev/fd/"+strconv.Itoa(3), p.Args...)
	cmd.Args[0] = p.Executable
	cmd.ExtraFiles = []*os.File{f}
	cmd.Env = []string{"PATH=" + detect.FixedPATH, "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	output := &boundedOutput{cancel: cancel}
	cmd.Stdout = output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Wait owns reaping and must run only after every group signal. There is no
	// CommandContext watcher that could signal concurrently with/after Wait.
	cmd.WaitDelay = 100 * time.Millisecond
	if err = cmd.Start(); err != nil {
		return detect.Output{}, err
	}
	err = finishProbe(ctx, &commandLease{cmd: cmd})
	stdout, overflow := output.result()
	result := detect.Output{Stdout: stdout, Exit: -1}
	if cmd.ProcessState != nil {
		result.Exit = cmd.ProcessState.ExitCode()
	}
	if overflow {
		return result, errOutputLimit
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, err
}
func (h *Host) Now() time.Time            { return time.Now() }
func (h *Host) Detector() detect.Detector { return detect.Detector{FS: h, Exec: h, Clock: h} }

// processLease keeps the command's PID owned until the final group signal.
type processLease interface {
	Exited() (bool, error)
	SignalGroup() error
	Release() error
	Wait() error
}

var errOwnershipLost = errors.New("version probe ownership lost")

type commandLease struct{ cmd *exec.Cmd }

func (c *commandLease) Exited() (bool, error) { return childExited(c.cmd.Process.Pid) }
func (c *commandLease) SignalGroup() error {
	err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
func (c *commandLease) Release() error { return c.cmd.Process.Release() }
func (c *commandLease) Wait() error    { return c.cmd.Wait() }

func finishProbe(ctx context.Context, child processLease) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var observationErr error
	for {
		if ctx.Err() != nil {
			observationErr = ctx.Err()
			break
		}
		exited, err := child.Exited()
		if errors.Is(err, errOwnershipLost) {
			// Never address a potentially reused PID/group. Release marks the os.Process
			// handle unusable before Cmd.Wait closes its bounded copy pipes; it cannot
			// issue a new OS wait for the no-longer-owned numeric PID.
			_ = child.Release()
			_ = child.Wait()
			return err
		}
		if err != nil {
			observationErr = err
			break
		}
		if exited {
			break
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	// No other goroutine can wait/release this private command. An exited but
	// unreaped child still reserves its PID/group identity through this signal.
	signalErr := child.SignalGroup()
	waitErr := child.Wait()
	if observationErr != nil {
		return observationErr
	}
	if signalErr != nil {
		return signalErr
	}
	return waitErr
}
