package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
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

// checkSocketDir refuses a directory that another user could enter or replace
// things in: it must be a real directory (not a link) owned by this user, with no
// permission for group or others.
func checkSocketDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("api: %s is not a directory", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("api: the socket directory %s is accessible to others (mode %04o): make it 0700, or the API socket could be reached by other users (D29)", dir, fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("api: the socket directory %s is owned by another user", dir)
	}
	return nil
}
