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
)

func TestLocalStageInitializerHasIndependentDeadline(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	tool := filepath.Join(t.TempDir(), "tool")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"start\",\"text\":\"Run: sh captured-initialize\",\"timestamp\":1}' >&2\nsleep .3\n"
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := newStageRunner(context.Background(), root, "unix:///tmp/test.sock", map[string]string{"sh": tool})
	if err != nil {
		t.Fatal(err)
	}
	runner.InitializationLabels = []string{"Run: sh captured-initialize"}
	runner.InitializationBudget = 30 * time.Millisecond
	_, err = runner.Run(context.Background(), "candidate_resources", "sh", nil, nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "initializer deadline exceeded") {
		t.Fatalf("host initialization escaped separate deadline: %v", err)
	}
}
