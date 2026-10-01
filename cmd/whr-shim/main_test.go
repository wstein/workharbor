package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShimRunAndKill(t *testing.T) {
	tmpDir := t.TempDir()
	pidfile := filepath.Join(tmpDir, "child.pid")

	// Build whr-shim binary for testing
	shimBin := filepath.Join(tmpDir, "whr-shim")
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", shimBin, ".") //nolint:gosec // test helper
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build whr-shim: %v", err)
	}

	// 1. Run a sleep command under whr-shim
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, shimBin, "run", "-pidfile", pidfile, "--", "sleep", "30") //nolint:gosec // test helper
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim run: %v", err)
	}

	// Wait for pidfile to be created
	var pid int
	for range 50 {
		data, err := os.ReadFile(pidfile) //nolint:gosec // test helper
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatalf("pidfile was not created or empty")
	}

	// Verify child process exists
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child process %d not alive: %v", pid, err)
	}

	// 2. Kill using whr-shim kill
	killCmd := exec.CommandContext(context.Background(), shimBin, "kill", "-pidfile", pidfile, "-grace", "200ms") //nolint:gosec // test helper
	out, err := killCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shim kill failed: %v, out: %s", err, string(out))
	}

	// Wait for shim process to exit
	_ = cmd.Wait()

	// Verify child process is dead
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("child process %d still alive after shim kill", pid)
	}
}
