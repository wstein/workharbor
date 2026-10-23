// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLocalDiscoveryCleansExitedWrapperDescendants(t *testing.T) {
	ps, err := exec.LookPath("ps")
	if err != nil {
		t.Skip("process-state probe requires ps")
	}
	for _, tc := range []struct{ name, ending, redirect string }{
		{"exited_with_pipe", "exit 0", ""},
		{"exited_without_pipe", "exit 0", " >/dev/null 2>&1"},
		{"cancelled_wrapper", "wait", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "pid")
			// The path comes only from t.TempDir; quote it as shell data.
			exe := fakeDocker(t, fmt.Sprintf("/bin/sleep 30%s &\nprintf '%%s' \"$!\" > '%s'\n%s\n", tc.redirect, strings.ReplaceAll(pidfile, "'", "'\\''"), tc.ending))
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			_, _ = (Client{Executable: exe}).read(ctx, "unix:///test/socket", "container", "ls", "--all", "--no-trunc", "--format", "{{json .}}")
			raw, err := os.ReadFile(pidfile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(raw))
			if err != nil || pid <= 0 {
				t.Fatalf("bad child PID: %q", raw)
			}
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			deadline := time.Now().Add(time.Second)
			for {
				out, err := exec.Command(ps, "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
				state := strings.TrimSpace(string(out))
				if state == "" {
					if err != nil {
						ee, ok := err.(*exec.ExitError)
						if !ok || ee.ExitCode() != 1 {
							t.Fatalf("process probe failed: %v", err)
						}
					}
					return
				}
				if strings.HasPrefix(state, "Z") {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("descendant remains alive after Docker adapter return: pid=%d state=%s", pid, state)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
