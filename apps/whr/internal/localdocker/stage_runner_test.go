// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalStagePhaseEnvironmentAndPinnedTool(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "CREDENTIAL_SENTINEL")
	t.Setenv("GIT_SSH_COMMAND", "HOST_COMMAND_SENTINEL")
	t.Setenv("LANG", "C.UTF-8")
	t.Setenv("ANCHORAGE_HOST_LABEL", "host label")
	root := t.TempDir()
	tool := filepath.Join(t.TempDir(), "tool")
	script := "#!/bin/sh\nprintf '%s\\n' \"$DOCKER_HOST\" \"$LANG\" \"$ANCHORAGE_HOST_LABEL\"\ntest -z \"${AWS_SECRET_ACCESS_KEY+x}\" || exit 8\ntest -z \"${GIT_SSH_COMMAND+x}\" || exit 9\ntest \"$HOME\" != \"\" || exit 10\ncat\n"
	if e := os.WriteFile(tool, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	runner, e := newStageRunner(root, "unix:///tmp/fixed.sock", map[string]string{"docker": tool})
	if e != nil {
		t.Fatal(e)
	}
	result, e := runner.Run(context.Background(), "smoke", "docker", []string{"exec", "--interactive"}, strings.NewReader("trusted stdin\n"), time.Second)
	if e != nil || !strings.Contains(string(result.Stdout), "unix:///tmp/fixed.sock\nC.UTF-8\nhost label\ntrusted stdin") {
		t.Fatalf("result=%q err=%v", result.Stdout, e)
	}
	if len(result.OutputDigest) != 71 {
		t.Fatalf("output digest %q", result.OutputDigest)
	}
	if e = os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = runner.Run(context.Background(), "smoke", "docker", nil, nil, time.Second); e == nil || !strings.Contains(e.Error(), "tool identity changed") {
		t.Fatalf("changed tool admitted: %v", e)
	}
}
func TestLocalStagePhaseOutputBoundAndCancellation(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(t.TempDir(), "tool")
	if e := os.WriteFile(tool, []byte("#!/bin/sh\nwhile :; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; done\n"), 0700); e != nil {
		t.Fatal(e)
	}
	runner, e := newStageRunner(root, "unix:///tmp/fixed.sock", map[string]string{"docker": tool})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = runner.Run(context.Background(), "container_hooks", "docker", nil, nil, time.Second); e == nil || !strings.Contains(e.Error(), "phase output quota") {
		t.Fatalf("unbounded output: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = runner.Run(ctx, "smoke", "docker", nil, nil, time.Second); e != context.Canceled {
		t.Fatalf("cancel: %v", e)
	}
}
