// SPDX-License-Identifier: EUPL-1.2

//go:build unix

package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

func shim(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// N7: a deadline kills the whole process group, not only the shim.
func TestDoctorGitKillsProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	g := shim(t, "/bin/sleep 30 &\necho $! > '"+pidfile+"'\n/bin/sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := doctor.SystemPorts(nil).GitVersion(ctx, g); err == nil {
		t.Fatal("expected an error after the deadline")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Error("the shim's grandchild survived the deadline")
	}
}

// D23: output is bounded.
func TestDoctorGitOutputBound(t *testing.T) {
	g := shim(t, "i=0\nwhile [ $i -lt 2000 ]; do printf aaaaaaaaaaaaaaaaaaaa; i=$((i+1)); done")
	line, err := doctor.SystemPorts(nil).GitVersion(context.Background(), g)
	if err != nil || len(line) == 0 || len(line) > 4096 {
		t.Errorf("len %d err %v", len(line), err)
	}
}

// D26: a grandchild that keeps the pipe open cannot hold the run past WaitDelay.
func TestDoctorGitWaitDelay(t *testing.T) {
	g := shim(t, "echo git version 2.43.0\n/bin/sleep 3 &")
	start := time.Now()
	line, _ := doctor.SystemPorts(nil).GitVersion(context.Background(), g)
	if time.Since(start) > 2*time.Second {
		t.Errorf("held for %v by a background child (%q)", time.Since(start), line)
	}
}
