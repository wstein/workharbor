//go:build unix

package hostgit

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// cacheMutexes holds one mutex per cache path, shared by every handle of the
// same cache in this process.
var cacheMutexes sync.Map

// lockFileName is the cache's own lock, never one of git's.
const lockFileName = "whr.lock"

// lock takes the cache's lock: the mutex of its path and, for other processes,
// an exclusive file lock. It waits for both with ctx. While it is held no other
// supervisor process runs git on the cache, so any git lock file is stale and is
// removed before the caller starts (design §4.5). The returned function
// releases the lock.
func (c *Cache) lock(ctx context.Context) (unlock func(), err error) {
	m, _ := cacheMutexes.LoadOrStore(c.path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	if err := acquire(ctx, mu.TryLock); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(c.path, lockFileName), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // inside the supervisor's own cache
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	if err := acquire(ctx, func() bool { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil }); err != nil { //nolint:gosec // a file descriptor fits an int
		_ = f.Close()
		mu.Unlock()
		return nil, err
	}
	recoverGitLocks(c.path)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // a file descriptor fits an int
		_ = f.Close()
		mu.Unlock()
	}, nil
}

// acquire polls try until it succeeds or ctx ends.
func acquire(ctx context.Context, try func() bool) error {
	for {
		if try() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// recoverGitLocks removes the lock files a killed git left in a cache. It is
// called only while the cache lock is held, and works through a root-scoped
// handle, so a link inside the cache cannot make it touch anything outside.
func recoverGitLocks(dir string) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	_ = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // a vanished entry is not a problem
		}
		if filepath.Ext(path) == ".lock" && filepath.Base(path) != lockFileName {
			_ = root.Remove(path)
		}
		return nil
	})
}
