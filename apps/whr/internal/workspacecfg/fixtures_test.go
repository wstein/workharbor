// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

const (
	manifestV1   = `{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`
	devcontainer = `{"name":"t","containerUser":"0:0","remoteUser":"1000:1000","image":"` + digest + `"}`
)

// validDir builds a minimal valid configuration directory.
func validDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "workharbor.json", manifestV1)
	write(t, dir, ".devcontainer/devcontainer.json", devcontainer)
	write(t, dir, "projects/.gitkeep", "")
	return dir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func remove(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, rel)); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, dir, rel, target string) {
	t.Helper()
	remove(t, dir, rel)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, rel)); err != nil {
		t.Fatal(err)
	}
}

func fifo(t *testing.T, dir, rel string) {
	t.Helper()
	remove(t, dir, rel)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, rel), 0o644); err != nil {
		t.Fatal(err)
	}
}

// socket binds a unix socket at dir/rel. Socket paths are short, so the bind
// goes through a descriptor path when the real one is too long.
func socket(t *testing.T, dir, rel string) {
	t.Helper()
	remove(t, dir, rel)
	parent := filepath.Dir(filepath.Join(dir, rel))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, rel)
	if len(path) >= 100 {
		f, err := os.Open(parent)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		path = "/proc/self/fd/" + itoa(int(f.Fd())) + "/" + filepath.Base(rel)
		if _, err := os.Stat("/proc/self/fd"); err != nil {
			t.Skip("no /proc/self/fd to bind a long socket path")
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	t.Cleanup(func() { l.Close() })
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func sparse(t *testing.T, dir, rel string, size int64) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Skipf("file system refuses a sparse file of %d bytes: %v", size, err)
	}
}

// validate runs the on-disk validator with a bounded wait so that a command
// blocking on a special file fails the test instead of hanging it.
func validate(t *testing.T, dir string, opt workspacecfg.Options) *workspacecfg.Report {
	t.Helper()
	type out struct {
		r   *workspacecfg.Report
		err error
	}
	ch := make(chan out, 1)
	go func() {
		r, err := workspacecfg.Validate(context.Background(), dir, opt)
		ch <- out{r, err}
	}()
	select {
	case o := <-ch:
		if o.err != nil {
			t.Fatalf("Validate: %v", o.err)
		}
		return o.r
	case <-time.After(60 * time.Second):
		t.Fatal("Validate did not return: it blocked")
	}
	return nil
}

func has(r *workspacecfg.Report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func find(r *workspacecfg.Report, code string) workspacecfg.Finding {
	for _, f := range r.Findings {
		if f.Code == code {
			return f
		}
	}
	return workspacecfg.Finding{}
}

func errorCodes(r *workspacecfg.Report) string {
	var fs []workspacecfg.Finding
	for _, f := range r.Findings {
		if f.Severity == workspacecfg.SevError {
			fs = append(fs, f)
		}
	}
	return codesOf(fs)
}

func isErr(err, target error) bool { return errors.Is(err, target) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
