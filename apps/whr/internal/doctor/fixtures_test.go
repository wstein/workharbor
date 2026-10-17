// SPDX-License-Identifier: EUPL-1.2

package doctor_test

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"syscall"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

const gib = uint64(1) << 30

// fakeFS is a fixed set of existing paths for Lstat.
type fakeFileInfo struct {
	name string
	mode fs.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

// world is a fully injected machine. The zero value is a healthy Linux host.
type world struct {
	mu       sync.Mutex
	os, arch string
	env      map[string]string
	home     string
	user     string
	host     string
	cwd      string

	// dial results by socket path; a missing path is not found.
	sockets map[string]error
	dialed  []string
	// dialDeadlines are the time left on each dial's context at call time.
	dialDeadlines []time.Duration
	dialAdvance   time.Duration
	gitDeadline   time.Duration
	links         map[string]string
	block         map[string]chan struct{} // FreeBytes blocks until closed
	budget        time.Duration

	existing map[string]bool // paths that Lstat finds
	files    map[string][]byte
	readErr  map[string]error
	readable map[string]error

	gitPath    string
	gitErr     error
	gitOut     string
	gitRunErr  error
	gitRuns    int
	gitAdvance time.Duration

	free    map[string]uint64
	freeErr map[string]error
	freed   []string

	now time.Time

	validator doctor.Validator
}

func healthy() *world {
	return &world{
		os: "linux", arch: "amd64", env: map[string]string{},
		home: "/home/alice", user: "alice", host: "devbox", cwd: "/work/project",
		sockets:  map[string]error{"/var/run/docker.sock": nil},
		existing: map[string]bool{}, files: map[string][]byte{},
		readErr: map[string]error{}, readable: map[string]error{},
		gitPath: "/usr/bin/git", gitOut: "git version 2.43.0",
		free: map[string]uint64{"/work/project": 10 * gib, "/var/lib/docker": 10 * gib}, freeErr: map[string]error{},
		now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
}

func (w *world) ports() doctor.Ports {
	return doctor.Ports{
		OS: w.os, Arch: w.arch,
		Getenv: func(k string) string { return w.env[k] },
		Home:   func() (string, error) { return w.home, nil },
		User:   func() string { return w.user },
		Host:   func() string { return w.host },
		Getwd:  func() (string, error) { return w.cwd, nil },
		Now:    func() time.Time { return w.now },
		Lstat: func(p string) (fs.FileInfo, error) {
			if w.existing[p] {
				return fakeFileInfo{name: p}, nil
			}
			return nil, &fs.PathError{Op: "lstat", Path: p, Err: syscall.ENOENT}
		},
		Budget: w.budget,
		Readlink: func(p string) (string, error) {
			if l, ok := w.links[p]; ok {
				return l, nil
			}
			return "", &fs.PathError{Op: "readlink", Path: p, Err: syscall.ENOENT}
		},
		DialUnix: func(ctx context.Context, p string) error {
			w.dialed = append(w.dialed, p)
			if d, ok := ctx.Deadline(); ok {
				w.dialDeadlines = append(w.dialDeadlines, time.Until(d))
			}
			w.now = w.now.Add(w.dialAdvance)
			err, ok := w.sockets[p]
			if !ok {
				return &fs.PathError{Op: "connect", Path: p, Err: syscall.ENOENT}
			}
			return err
		},
		ReadFile: func(p string, _ int64) ([]byte, error) {
			if err := w.readErr[p]; err != nil {
				return nil, err
			}
			if b, ok := w.files[p]; ok {
				return b, nil
			}
			return nil, &fs.PathError{Op: "open", Path: p, Err: syscall.ENOENT}
		},
		CanRead: func(p string) error { return w.readable[p] },
		FindGit: func() (string, error) { return w.gitPath, w.gitErr },
		GitVersion: func(ctx context.Context, _ string) (string, error) {
			w.gitRuns++
			if d, ok := ctx.Deadline(); ok {
				w.gitDeadline = time.Until(d)
			}
			w.now = w.now.Add(w.gitAdvance)
			return w.gitOut, w.gitRunErr
		},
		FreeBytes: func(p string) (uint64, error) {
			w.mu.Lock()
			w.freed = append(w.freed, p)
			ch := w.block[p]
			w.mu.Unlock()
			if ch != nil {
				<-ch
			}
			if err := w.freeErr[p]; err != nil {
				return 0, err
			}
			if v, ok := w.free[p]; ok {
				return v, nil
			}
			return 0, errors.New("unmeasurable")
		},
		Validator: w.validator,
	}
}

var (
	errDenied  = &fs.PathError{Op: "connect", Path: "x", Err: syscall.EACCES}
	errRefused = &fs.PathError{Op: "connect", Path: "x", Err: syscall.ECONNREFUSED}
)

func statusOf(r doctor.Report, id string) doctor.Check {
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	return doctor.Check{}
}
