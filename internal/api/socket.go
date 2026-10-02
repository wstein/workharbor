package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/wstein/workharbor/internal/config"
)

// SocketName is the API's socket in the state directory (D29, §7.5).
const SocketName = config.APISocketName

// maxSocketPath is what a unix socket path may be: sockaddr_un has 104 bytes on
// macOS, one of them the terminator.
const maxSocketPath = 100

// ListenSocket binds the API's unix socket (D29, §7.5). The JSON API is served only
// here: a socket in a directory only the supervisor's user can enter is out of reach
// of the forwarded listener and of every guest, so a leaked token cannot be used
// from the phone network. The directory must be owned by this user and closed to
// everyone else (it is created 0700 when it is missing), the socket is 0600, and a
// socket that is already served is not taken over. A stale socket file from an
// earlier run is replaced.
func ListenSocket(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("api: the socket path %q is not absolute", path)
	}
	if len(path) > maxSocketPath {
		return nil, fmt.Errorf("api: the socket path %q is %d bytes: a unix socket path is at most %d; set a shorter state_dir", path, len(path), maxSocketPath)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("api: the socket directory: %w", err)
	}
	if err := checkSocketDir(dir); err != nil {
		return nil, err
	}
	// One supervisor at a time: the lock is held for as long as the listener lives,
	// so a second `whr serve` cannot remove the socket the first one serves.
	lock, err := lockDir(dir)
	if err != nil {
		return nil, err
	}
	ln, err := listenLocked(path)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	return &lockedListener{Listener: ln, lock: lock}, nil
}

// LockName is the lock file next to the socket.
const LockName = SocketName + ".lock"

// lockDir takes an exclusive lock on a file in the state directory, without
// waiting and without following a link.
func lockDir(dir string) (*os.File, error) {
	//nolint:gosec // the name is the constant LockName in the state directory that was just checked
	f, err := os.OpenFile(filepath.Join(dir, LockName), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("api: the lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("api: another `whr serve` holds %s: is it already running?", filepath.Join(dir, LockName))
		}
		return nil, fmt.Errorf("api: lock %s: %w", f.Name(), err)
	}
	return f, nil
}

// lockedListener releases the lock when the listener is closed, after the socket
// file is gone.
type lockedListener struct {
	net.Listener
	lock *os.File
	once sync.Once
}

func (l *lockedListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { _ = l.lock.Close() })
	return err
}

// listenLocked binds the socket; the caller holds the lock.
func listenLocked(path string) (net.Listener, error) {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("api: %s exists and is not a socket", path)
		}
		d := net.Dialer{Timeout: time.Second}
		if c, err := d.DialContext(context.Background(), "unix", path); err == nil {
			_ = c.Close()
			return nil, fmt.Errorf("api: %s is already served: is `whr serve` running?", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("api: remove the stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// The socket is created under a umask that leaves it to its owner, so there is
	// no moment when it is open to others, and it is checked afterwards.
	old := syscall.Umask(0o177)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, fmt.Errorf("api: listen on %s: %w", path, err)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode().Perm()&0o077 != 0 {
		_ = ln.Close()
		return nil, fmt.Errorf("api: the socket %s is reachable by others: refusing to serve", path)
	}
	return ln, nil
}

// checkSocketDir applies the rule the configuration check applies to state_dir.
func checkSocketDir(dir string) error {
	if err := config.CheckStateDir(dir); err != nil {
		return fmt.Errorf("api: %w", err)
	}
	return nil
}
