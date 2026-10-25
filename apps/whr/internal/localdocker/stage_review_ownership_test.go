//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLocalStageReviewSocketAliasesShareLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	first, err := openStageStore(root, "daemon", "unix:///tmp/first.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	alias, err := openStageStore(root, "daemon", "unix:///tmp/alias.sock")
	if alias != nil {
		alias.Close()
	}
	if exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), "operation lock is held") {
		t.Fatalf("socket alias bypassed daemon lock: %v", err)
	}
	other, err := openStageStore(root, "other-daemon", "unix:///tmp/first.sock")
	if err != nil {
		t.Fatalf("unrelated daemon blocked: %v", err)
	}
	other.Close()
}
func TestLocalStageReviewObservationFailureKillsGroup(t *testing.T) {
	var signals []syscall.Signal
	group := &stageProcessGroup{owned: true, signal: func(sig syscall.Signal) error { signals = append(signals, sig); return nil }}
	if err := group.terminate(); err != nil {
		t.Fatal(err)
	}
	group.releaseBeforeReap(false)
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("observation failure released surviving group: %v", signals)
	}
	if err := group.terminate(); err != os.ErrProcessDone {
		t.Fatalf("released group reused: %v", err)
	}
}
func TestLocalStageReviewInitializerContextPrecedesProgress(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nsleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := newStageRunner(context.Background(), root, "unix:///tmp/docker.sock", map[string]string{"docker": tool})
	if err != nil {
		t.Fatal(err)
	}
	runner.InitializationLabels = []string{"fixture initializer"}
	runner.InitializationBudget = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, "candidate_resources", "docker", nil, nil, time.Second)
	if exitcode.FromError(err) != exitcode.Timeout || err != context.DeadlineExceeded {
		t.Fatalf("progress masked parent deadline: %v", err)
	}
}
func TestLocalStageReviewToolManifestExcludesDriverAfterApply(t *testing.T) {
	client, opts, ports, _ := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	stage, err := client.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if err != nil || result.Status != "staged_verified" {
		t.Fatalf("stage: %+v %v", result, err)
	}
	if _, ok := stage.runner.Tools["docker_driver"]; !ok {
		t.Fatal("test did not exercise generated driver")
	}
	if _, ok := stage.receipt.ToolManifest["docker_driver"]; ok {
		t.Fatal("runtime driver leaked into trusted receipt tools")
	}
	if stage.settings.SmokeDigest != stageBytesDigest([]byte("printf smoke\n")) {
		t.Fatalf("verified settings lost admitted smoke digest: %q", stage.settings.SmokeDigest)
	}
}

func TestLocalStageReviewObservationFailureKillsWithoutTimer(t *testing.T) {
	var signals []syscall.Signal
	group := &stageProcessGroup{owned: true, signal: func(sig syscall.Signal) error { signals = append(signals, sig); return nil }}
	group.releaseBeforeReap(false)
	if len(signals) != 1 || signals[0] != syscall.SIGKILL {
		t.Fatalf("leader observation failure leaked descendants: %v", signals)
	}
	if err := group.terminate(); err != os.ErrProcessDone {
		t.Fatalf("released group reused: %v", err)
	}
}
