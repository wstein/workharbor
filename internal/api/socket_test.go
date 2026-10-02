package api

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "whr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func TestTheSocketIsPrivateAndServesTheAPI(t *testing.T) {
	r := newRig(t)
	dir := filepath.Join(shortDir(t), "state")
	path := filepath.Join(dir, SocketName)
	ln, err := ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("the directory it created is %o, want 0700", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 || fi.Mode()&os.ModeSocket == 0 {
		t.Errorf("the socket is %v, want a 0600 socket", fi.Mode())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.srv.Serve(ctx, ln) }()
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", path)
	}}, Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://whr/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("health over the socket = %d", resp.StatusCode)
	}
}

func TestTheSocketRefusesAnOpenDirectoryAndASocketAlreadyServed(t *testing.T) {
	base := shortDir(t)
	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o755); err != nil { //nolint:gosec // a deliberately open directory
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o755); err != nil { //nolint:gosec // the umask may have narrowed it
		t.Fatal(err)
	}
	if _, err := ListenSocket(filepath.Join(open, SocketName)); err == nil || !strings.Contains(err.Error(), "accessible to others") {
		t.Errorf("a directory open to others: %v", err)
	}
	group := filepath.Join(base, "group")
	if err := os.Mkdir(group, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(group, 0o710); err != nil { //nolint:gosec // group may enter
		t.Fatal(err)
	}
	if _, err := ListenSocket(filepath.Join(group, SocketName)); err == nil {
		t.Error("a directory a group may enter was accepted")
	}

	dir := filepath.Join(base, "ok")
	path := filepath.Join(dir, SocketName)
	ln, err := ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	// a second supervisor is stopped by the lock, before it can touch the socket
	if _, err := ListenSocket(path); err == nil || !strings.Contains(err.Error(), "another `whr serve`") {
		t.Errorf("a second listener: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("a refused second listener removed the first one's socket: %v", err)
	}
	_ = ln.Close() // a closed listener removes its socket and its lock; a leftover file is stale
	if ln2, err := ListenSocket(path); err != nil {
		t.Errorf("after the first closed: %v", err)
	} else {
		_ = ln2.Close()
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenSocket(path); err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Errorf("a regular file in the way: %v", err)
	}
	_ = os.Remove(path)

	// a path that does not fit a sockaddr, and a relative one
	if _, err := ListenSocket(filepath.Join(base, strings.Repeat("d", 90), SocketName)); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("a long path: %v", err)
	}
	if _, err := ListenSocket("api.sock"); err == nil {
		t.Error("a relative path was accepted")
	}
}

// A socket served by something that holds no lock (an older whr) is not taken over
// either: the dial says so.
func TestASocketServedWithoutTheLockIsNotTakenOver(t *testing.T) {
	dir, err := os.MkdirTemp("", "whr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, SocketName)
	var lc net.ListenConfig
	other, err := lc.Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if _, err := ListenSocket(path); err == nil || !strings.Contains(err.Error(), "already served") {
		t.Errorf("a socket served without the lock: %v", err)
	}
}
