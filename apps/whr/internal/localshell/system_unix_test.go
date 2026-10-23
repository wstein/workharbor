// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localshell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalShellDirectFilesAndClientEnvironment(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
test -f /dev/stdin && test -f /dev/stdout && test -f /dev/stderr || exit 98
test -z "$OPENAI_API_KEY$ANTHROPIC_API_KEY$GITHUB_TOKEN$DOCKER_HOST$DOCKER_CONTEXT$DOCKER_AUTH_CONFIG$DOCKER_TLS_VERIFY" || exit 97
test -d "$DOCKER_CONFIG" && test ! -e "$DOCKER_CONFIG/config.json" || exit 96
printf 'RAW\033[31m\007\n'
printf 'CHILD_STDERR\n' >&2
exit 7
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, k := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GITHUB_TOKEN", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_AUTH_CONFIG", "DOCKER_TLS_VERIFY"} {
		t.Setenv(k, "canary-secret")
	}
	f := func(name string) *os.File {
		file, err := os.CreateTemp(t.TempDir(), name)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { file.Close() })
		return file
	}
	in, out, diag := f("in"), f("out"), f("err")
	plan, err := NewExecPlan("unix:///test/socket", validMetadata(), "fish")
	if err != nil {
		t.Fatal(err)
	}
	status, err := SystemPorts().Exec(context.Background(), plan, Streams{Stdin: in, Stdout: out, Stderr: diag})
	if err != nil || status != 7 {
		t.Fatalf("%d %v", status, err)
	}
	raw, _ := os.ReadFile(out.Name())
	if string(raw) != "RAW\x1b[31m\a\n" {
		t.Fatalf("stream transformed: %q", raw)
	}
	raw, _ = os.ReadFile(diag.Name())
	if string(raw) != "CHILD_STDERR\n" || strings.Contains(string(raw), "canary") {
		t.Fatalf("%q", raw)
	}
}

func TestLocalShellSignalStatusFromDockerProcess(t *testing.T) {
	for _, tc := range []struct {
		signal string
		status int
	}{{"KILL", 137}, {"TERM", 143}} {
		t.Run(tc.signal, func(t *testing.T) {
			bin := t.TempDir()
			script := fmt.Sprintf("#!/bin/sh\nkill -%s $$\nexit 99\n", tc.signal)
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer devnull.Close()
			plan, err := NewExecPlan("unix:///test/socket", validMetadata(), "fish")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			status, err := SystemPorts().Exec(ctx, plan, Streams{Stdin: devnull, Stdout: devnull, Stderr: devnull})
			if err != nil || status != tc.status {
				t.Fatalf("signal %s: status=%d error=%v; want %d", tc.signal, status, err, tc.status)
			}
		})
	}
}

func TestLocalShellCancellationLetsDockerRestoreTerminal(t *testing.T) {
	bin := t.TempDir()
	ready := filepath.Join(bin, "ready")
	observed := filepath.Join(bin, "signal")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nset -- " + quote(observed) + "\ntrap 'printf TERM > \"$1\"; exit 0' TERM\nprintf READY > " + quote(ready) + "\nwhile :; do :; done\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	file, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	plan, err := NewExecPlan("unix:///test/socket", validMetadata(), "fish")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := SystemPorts().Exec(ctx, plan, Streams{Stdin: file, Stdout: file, Stderr: file})
		done <- err
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if data, err := os.ReadFile(ready); err == nil && string(data) == "READY" {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("child ended before ready: %v", err)
		case <-ctx.Done():
			t.Fatal("child never became ready")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Docker child was not reaped")
	}
	data, err := os.ReadFile(observed)
	if err != nil || string(data) != "TERM" {
		t.Fatalf("child did not restore terminal on SIGTERM: %q %v", data, err)
	}
}
