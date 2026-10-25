// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestLocalStageCaptureObservationFailureKillsOwnedGroup(t *testing.T) {
	for _, withTimer := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_timer", true: "with_timer"}[withTimer], func(t *testing.T) {
			var signals []syscall.Signal
			group := &stageProcessGroup{owned: true, signal: func(sig syscall.Signal) error { signals = append(signals, sig); return nil }}
			if withTimer {
				if err := group.terminate(); err != nil {
					t.Fatal(err)
				}
			}
			group.releaseBeforeReap(false)
			if len(signals) == 0 || signals[len(signals)-1] != syscall.SIGKILL {
				t.Fatalf("owned group released without final kill: %v", signals)
			}
			before := len(signals)
			if err := group.terminate(); err != os.ErrProcessDone {
				t.Fatalf("released PID ownership reused: %v", err)
			}
			group.releaseBeforeReap(true)
			if len(signals) != before {
				t.Fatal("signaled an already released group")
			}
		})
	}
}

func TestLocalStageCaptureNativeWaitWithoutReap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := runStageGit(cmd); err != nil {
		t.Fatalf("native wait/owned-group cleanup: %v", err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		t.Fatal("native wait consumed leader before exec.Cmd.Wait")
	}
	if err := stageWaitLeader(cmd.Process.Pid); err == nil {
		t.Fatal("reaped leader remained waitable")
	}
}
