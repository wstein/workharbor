// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

// Package profile owns private host-profile and real-run audit storage. Open
// requires an explicit trusted home root; it never discovers the process HOME.
package profile

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	ErrUnsafe   = errors.New("host_setup.target_unsafe")
	ErrLocked   = errors.New("host_setup.run_locked")
	ErrProtocol = errors.New("host_setup.protocol_unwritable")
)

const state = ".local/state/whr"
const profilePath = ".config/whr/host-profile.json"

// Store pins the caller's explicit root. Closing it does not close live runs.
type Store struct {
	mu   sync.Mutex
	root *os.File
}

func Open(home string) (*Store, error) {
	if !strings.HasPrefix(home, "/") {
		return nil, ErrUnsafe
	}
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	return &Store{root: os.NewFile(uintptr(fd), "host-profile-root")}, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return nil
	}
	err := s.root.Close()
	s.root = nil
	return err
}
func (s *Store) duplicate() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return -1, ErrUnsafe
	}
	fd, err := unix.FcntlInt(s.root.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, ErrUnsafe
	}
	return fd, nil
}

// directory opens every component without following links. Newly created dirs
// have exact 0700 modes regardless of umask. A private leaf must already be0700.
func directory(root int, p string, create, private bool) (int, error) {
	fd, err := unix.FcntlInt(uintptr(root), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, ErrUnsafe
	}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			unix.Close(fd)
			return -1, ErrUnsafe
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) && create {
			e = unix.Mkdirat(fd, part, 0700)
			if e == nil {
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if e == nil {
					e = unix.Fchmod(next, 0700)
					if e == nil {
						e = unix.Fsync(next)
					}
				}
				if e == nil {
					e = unix.Fsync(fd)
				}
			}
		}
		unix.Close(fd)
		if e != nil {
			if next >= 0 {
				unix.Close(next)
			}
			if errors.Is(e, unix.ENOENT) && !create {
				return -1, fs.ErrNotExist
			}
			return -1, ErrUnsafe
		}
		fd = next
		if private && i == len(parts)-1 {
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil || st.Mode&07777 != 0700 {
				unix.Close(fd)
				return -1, ErrUnsafe
			}
		}
	}
	return fd, nil
}
func regular(fd int, mode uint32) bool {
	var st unix.Stat_t
	return unix.Fstat(fd, &st) == nil && st.Mode&unix.S_IFMT == unix.S_IFREG && uint32(st.Mode)&07777 == mode && st.Nlink == 1
}
func exclusive(parent int, name string) (*os.File, error) {
	fd, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_APPEND|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, ErrProtocol
	}
	f := os.NewFile(uintptr(fd), "run-protocol")
	if unix.Fchmod(fd, 0600) != nil || !regular(fd, 0600) || f.Sync() != nil || unix.Fsync(parent) != nil {
		f.Close()
		return nil, ErrProtocol
	}
	return f, nil
}
func appendSync(f *os.File, b []byte) error {
	n, err := f.Write(b)
	if err != nil || n != len(b) {
		return ErrProtocol
	}
	if f.Sync() != nil {
		return ErrProtocol
	}
	return nil
}

// Run owns the flock and append-only protocol handles until Close. Warning is
// consumed by the command adapter on stderr; this package has no output stream.
// It is safe to close a run repeatedly; a closed/failed run cannot write again.
type Run struct {
	mu                         sync.Mutex
	root                       int
	lock, json, text           *os.File
	id                         string
	clock                      time.Time
	finished, closed, poisoned bool
	Warning                    bool
}

func (s *Store) Begin(start Start, now time.Time) (*Run, error) {
	if !validStart(start) || now.IsZero() {
		return nil, ErrUnsafe
	}
	root, err := s.duplicate()
	if err != nil {
		return nil, err
	}
	r := &Run{root: root, id: start.RunID, clock: now}
	fail := func(e error) (*Run, error) { r.closeFiles(); return nil, e }
	dir, err := directory(root, state, true, true)
	if err != nil {
		return fail(ErrProtocol)
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, "run.lock", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	created := err == nil
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Openat(dir, "run.lock", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return fail(ErrLocked)
	}
	r.lock = os.NewFile(uintptr(fd), "run-lock")
	if created && unix.Fchmod(fd, 0600) != nil {
		return fail(ErrLocked)
	}
	if !regular(fd, 0600) {
		return fail(ErrLocked)
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fail(ErrLocked)
	}
	if unix.Fchmod(fd, 0600) != nil || r.lock.Sync() != nil || unix.Fsync(dir) != nil {
		return fail(ErrProtocol)
	}
	protocolDir, err := directory(root, state+"/protocols", true, true)
	if err != nil {
		return fail(ErrProtocol)
	}
	defer unix.Close(protocolDir)
	// Duplicate to count without changing the descriptor used for creation.
	countFD, err := unix.Openat(protocolDir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fail(ErrProtocol)
	}
	countFile := os.NewFile(uintptr(countFD), "protocol-count")
	count := 0
	for {
		names, e := countFile.Readdirnames(256)
		for _, name := range names {
			if runIDPattern.MatchString(strings.TrimSuffix(name, ".jsonl")) && strings.HasSuffix(name, ".jsonl") {
				count++
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			countFile.Close()
			return fail(ErrProtocol)
		}
	}
	countFile.Close()
	r.Warning = count+1 > 500
	r.json, err = exclusive(protocolDir, start.RunID+".jsonl")
	if err != nil {
		return fail(ErrProtocol)
	}
	r.text, err = exclusive(protocolDir, start.RunID+".txt")
	if err != nil {
		return fail(ErrProtocol)
	}
	if err = r.write(map[string]any{"type": "run_start", "command": start.Command, "platform": start.Platform, "catalogue_version": start.CatalogueVersion}); err != nil {
		return fail(err)
	}
	if err = r.write(map[string]any{"type": "selection", "answer": start.Answer, "selected": nonNil(start.Selected), "opted_out": nonNil(start.OptedOut)}); err != nil {
		return fail(err)
	}
	return r, nil
}
func (r *Run) closeFiles() error {
	var err error
	for _, f := range []*os.File{r.json, r.text, r.lock} {
		if f != nil {
			if e := f.Close(); e != nil {
				err = ErrProtocol
			}
		}
	}
	r.json = nil
	r.text = nil
	r.lock = nil
	if r.root >= 0 {
		unix.Close(r.root)
		r.root = -1
	}
	return err
}
func (r *Run) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	var err error
	if !r.finished && !r.poisoned && r.json != nil {
		err = r.finish(72, []string{}, []string{})
	}
	r.closed = true
	if e := r.closeFiles(); e != nil {
		err = e
	}
	return err
}
