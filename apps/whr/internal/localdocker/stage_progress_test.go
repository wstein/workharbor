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

func TestLocalStageInitializerProgressSeparatesResourceBudget(t *testing.T) {
	start := "printf '%s\\n' '{\"type\":\"start\",\"text\":\"Run: sh captured-initialize\",\"timestamp\":1}' >&2\n"
	stop := "printf '%s\\n' '{\"type\":\"stop\",\"text\":\"Run: sh captured-initialize\",\"startTimestamp\":1,\"timestamp\":2}' >&2\n"
	for _, tc := range []struct {
		name, body, reason string
		resource           time.Duration
	}{
		{"resource retains own budget", start + stop + "sleep .09\n", "", time.Second},
		{"resource deadline", start + stop + "sleep .3\n", "resource deadline exceeded", 30 * time.Millisecond},
		{"missing progress", "exit 0\n", "initializer progress unavailable or ambiguous", time.Second},
		{"raw spoof", "printf '%s\\n' '{\"type\":\"raw\",\"text\":\"Run: sh captured-initialize\",\"timestamp\":1}' >&2\n", "initializer progress unavailable or ambiguous", time.Second},
		{"unmatched stop", start + strings.Replace(stop, `startTimestamp":1`, `startTimestamp":9`, 1), "initializer progress unavailable or ambiguous", time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			os.Chmod(root, 0700)
			tool := filepath.Join(t.TempDir(), "tool")
			if err := os.WriteFile(tool, []byte("#!/bin/sh\n"+tc.body), 0700); err != nil {
				t.Fatal(err)
			}
			runner, err := newStageRunner(context.Background(), root, "unix:///tmp/test.sock", map[string]string{"sh": tool})
			if err != nil {
				t.Fatal(err)
			}
			runner.InitializationLabels = []string{"Run: sh captured-initialize"}
			runner.InitializationBudget = 30 * time.Millisecond
			_, err = runner.Run(context.Background(), "candidate_resources", "sh", nil, nil, tc.resource)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("progress outcome: %v", err)
			}
		})
	}
}
