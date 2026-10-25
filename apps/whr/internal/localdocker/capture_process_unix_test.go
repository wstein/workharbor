// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"os"
	"syscall"
	"testing"
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
