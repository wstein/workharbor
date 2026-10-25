//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalStagePinsOfficialHostShellInsteadOfAmbientSh(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"docker", "node", "devcontainer", "git", "sh", "bash"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ports, err := nativeStagePreparationPorts(Client{})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if ports.Tools["sh"] != actual {
		t.Fatalf("admitted ambient shell %q instead of actual official CLI shell %q", ports.Tools["sh"], actual)
	}
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := newStageRunner(context.Background(), root, "unix:///fixture.sock", map[string]string{"sh": ports.Tools["sh"]})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := stageFileDigest(context.Background(), actual)
	if err != nil {
		t.Fatal(err)
	}
	if runner.Tools["sh"].Path != actual || runner.Tools["sh"].Digest != expected {
		t.Fatal("actual official shell is not physically hash-bound")
	}
	// Simulate a drifted recorded binding, without modifying or executing /bin/sh.
	identity := runner.Tools["sh"]
	identity.Digest = stageBytesDigest([]byte("different shell bytes"))
	runner.Tools["sh"] = identity
	_, err = runner.Run(context.Background(), "metadata", "sh", []string{"-c", "exit 0"}, nil, time.Second)
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "tool identity changed before metadata") {
		t.Fatalf("actual shell drift binding: %v", err)
	}
}
