package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// buildShim builds whr-shim into a temporary directory.
func buildShim(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "whr-shim")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", bin, ".") //nolint:gosec // test helper
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build whr-shim: %v\n%s", err, out)
	}
	return bin
}

// waitForPID polls the pidfile until it holds a PID. The deadline is generous
// because the shim starts slowly under -race with parallel packages.
func waitForPID(t *testing.T, pidfile string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidfile); err == nil { //nolint:gosec // test helper
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pidfile %s was not written within 10s", pidfile)
	return 0
}

func TestShimRunAndKill(t *testing.T) {
	shim := buildShim(t)
	pidfile := filepath.Join(t.TempDir(), "child.pid")

	cmd := exec.CommandContext(context.Background(), shim, "run", "-pidfile", pidfile, "--", "sleep", "30") //nolint:gosec // test helper
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim run: %v", err)
	}
	pid := waitForPID(t, pidfile)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child process %d not alive: %v", pid, err)
	}

	kill := exec.CommandContext(context.Background(), shim, "kill", "-pidfile", pidfile, "-grace", "200ms") //nolint:gosec // test helper
	if out, err := kill.CombinedOutput(); err != nil {
		t.Fatalf("shim kill failed: %v, out: %s", err, out)
	}
	_ = cmd.Wait()

	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("child process %d still alive after shim kill", pid)
	}
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Errorf("pidfile left behind after kill: %v", err)
	}
}

// A pidfile that outlives its child would make a later kill signal whatever
// process group reuses that ID, so run must remove it on every exit path,
// including a failing or signalled child.
func TestShimRemovesPidfileWhenChildFails(t *testing.T) {
	shim := buildShim(t)
	for _, tc := range []struct {
		name     string
		args     []string
		wantCode int
	}{
		{"exit status", []string{"sh", "-c", "exit 3"}, 3},
		{"killed by signal", []string{"sh", "-c", "kill -TERM $$"}, 128 + int(syscall.SIGTERM)},
		{"success", []string{"true"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "child.pid")
			args := append([]string{"run", "-pidfile", pidfile, "--"}, tc.args...)
			err := exec.CommandContext(context.Background(), shim, args...).Run() //nolint:gosec // test helper
			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("run: %v", err)
			}
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
				t.Errorf("pidfile left behind: %v", err)
			}
		})
	}
}

// writePIDFile must never leave a partly written file for kill to read.
func TestWritePIDFileIsAtomic(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "child.pid")
	if err := writePIDFile(pidfile, 12345); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidfile) //nolint:gosec // test helper
	if err != nil || strings.TrimSpace(string(data)) != "12345" {
		t.Fatalf("pidfile = %q, %v; want 12345", data, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary files left in %s: %v", dir, entries)
	}
}

func TestChownGivesOneDirectoryToANumericUser(t *testing.T) {
	dir := t.TempDir()
	me := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	if err := chownCmd([]string{"-owner", me, dir}); err != nil {
		t.Fatalf("chown to the current user: %v", err)
	}
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "l")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"root uid":     {"-owner", "0:0", dir},
		"a name":       {"-owner", "agent:agent", dir},
		"no gid":       {"-owner", "1000", dir},
		"a file":       {"-owner", me, file},
		"a link":       {"-owner", me, link},
		"two paths":    {"-owner", me, dir, dir},
		"missing":      {"-owner", me, filepath.Join(dir, "nope")},
		"no directory": {"-owner", me},
	} {
		if err := chownCmd(args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
