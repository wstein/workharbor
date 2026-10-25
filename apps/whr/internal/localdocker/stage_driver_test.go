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

func TestLocalStageDriverPinsEndpointAndPromoteImage(t *testing.T) {
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	native := filepath.Join(t.TempDir(), "docker")
	if e := os.WriteFile(native, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	runner, e := newStageRunner(context.Background(), root, "unix:///tmp/fixed.sock", map[string]string{"docker": native, "sh": "/bin/sh"})
	if e != nil {
		t.Fatal(e)
	}
	image := "sha256:" + strings.Repeat("a", 64)
	driver, e := runner.DockerDriver("promote", image)
	if e != nil {
		t.Fatal(e)
	}
	runner.Tools["docker_driver"] = stageToolIdentity{Path: driver, Digest: mustStageToolDigest(t, driver)}
	out, e := runner.Run(context.Background(), "native_run", "docker_driver", []string{"run", "--name", "primary", "--mount", "type=bind,source=/original,target=/workspaces/repo", image, "/bin/sh"}, nil, time.Second)
	if e != nil || !strings.HasPrefix(string(out.Stdout), "--host\nunix:///tmp/fixed.sock\nrun\n--pull\nnever\n") {
		t.Fatalf("driver output %q %v", out.Stdout, e)
	}
	for _, args := range [][]string{{"pull", image}, {"build", "."}, {"run", "--name", "primary", "sha256:" + strings.Repeat("b", 64)}, {"--host", "unix:///tmp/foreign.sock", "info"}, {"run", "--pull=always", image}} {
		if _, e = runner.Run(context.Background(), "native_run", "docker_driver", args, nil, time.Second); e == nil {
			t.Fatalf("unsafe promote vector admitted: %v", args)
		}
	}
}
func mustStageToolDigest(t *testing.T, path string) string {
	t.Helper()
	v, e := stageFileDigest(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
