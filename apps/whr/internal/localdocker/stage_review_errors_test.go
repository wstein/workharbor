//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalStageReviewAdmissionPreservesContext(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		code  exitcode.Code
	}{
		{"deadline", context.DeadlineExceeded, exitcode.Timeout},
		{"interrupt", context.Canceled, exitcode.Interrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var cancel context.CancelFunc
			if tc.cause == context.Canceled {
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			} else {
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			err := admitStageToolVersions(ctx, &stageRunner{})
			if exitcode.FromError(err) != tc.code || err != tc.cause {
				t.Fatalf("context replaced by capability refusal: %v (exit %d)", err, exitcode.FromError(err))
			}
		})
	}
}
func TestLocalStageReviewApplyAdmissionPreservesContext(t *testing.T) {
	c, opts, ports, _ := fixtureStageLifecycle(t, false)
	stage, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := stage.Apply(ctx, LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.Interrupted || err != context.Canceled || result.Status != "refused" {
		t.Fatalf("cancel before allocation: %+v %v", result, err)
	}
	if _, err = os.Stat(ports.StateRoot); !os.IsNotExist(err) {
		t.Fatalf("pre-effect cancellation created state: %v", err)
	}
}
func fixtureStageReviewAttempt(t *testing.T, phase, action string) *LocalStage {
	t.Helper()
	store, err := openStageStore(filepath.Join(t.TempDir(), "state"), "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	op := strings.Repeat("1", 32)
	if err = store.Begin(op, "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"allocating", "candidate_resources", "candidate_hooks", "verifying"} {
		if err = store.Advance(next, "", "observed"); err != nil {
			t.Fatal(err)
		}
		if next == phase {
			break
		}
	}
	if err = store.Advance(phase, action, "attempted"); err != nil {
		t.Fatal(err)
	}
	stage := &LocalStage{store: store, operationID: op, receipt: &stageReceipt{OperationID: op, ToolManifest: map[string]stageToolIdentity{}, OutputInventory: []StageOutputEntry{}, OriginBinding: []StageRepositoryBinding{}, CandidateGitVersion: []stageCandidateGitVersion{}, CloneRepositories: []StageRepository{}, ExportedRefs: []StageResultRef{}, ImportedObjects: []StageGitObjectsTarget{}, ResultRefTransactions: []StageGitRefsTarget{}, ActionInventory: []StageEffect{{Action: action, State: "attempted", Target: StageTarget{Resource: &StageResource{Kind: "container", ID: stageString(strings.Repeat("c", 64)), DaemonID: "daemon"}}}}}}
	if err = store.saveReceipt(stage.receipt, true); err != nil {
		t.Fatal(err)
	}
	return stage
}
func TestLocalStageReviewPartialRetainsCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  string
	}{
		{"deadline", context.DeadlineExceeded, "deadline"},
		{"interrupt", context.Canceled, "interrupt"},
		{"driver", stageConflict("Docker driver refused: native_mount\n\x1b[31m"), "native_mount<U+000A><U+001B>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stage := fixtureStageReviewAttempt(t, "allocating", "create_volume")
			_, err := stage.stagePartial("home creation uncertain", tc.cause)
			if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), tc.want) || strings.ContainsAny(err.Error(), "\n\x1b") {
				t.Fatalf("partial cause lost or unsafe: %v", err)
			}
		})
	}
}
func TestLocalStageReviewFailedSmokeIsUncertain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  string
	}{
		{"failure", stageConflict("phase command failed: smoke"), "phase command failed: smoke"},
		{"deadline", context.DeadlineExceeded, "deadline"},
		{"interrupt", context.Canceled, "interrupt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stage := fixtureStageReviewAttempt(t, "verifying", "smoke")
			result, err := stage.stageUnverified("failed_smoke", "sha256:"+strings.Repeat("d", 64), "sha256:"+strings.Repeat("b", 64), tc.cause)
			if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("smoke failure lost cause: %+v %v", result, err)
			}
			if len(result.Effects) != 1 || result.Effects[0].State != "uncertain" {
				t.Fatalf("failed smoke falsely observed: %+v", result.Effects)
			}
			records, err := stage.store.Records()
			if err != nil {
				t.Fatal(err)
			}
			last := records[len(records)-1]
			if last.State != "verifying" || last.Action != "smoke" || last.Observation != "uncertain" {
				t.Fatalf("failed smoke journal invented observation: %+v", last)
			}
			receipt, err := stage.store.loadReceipt(stage.operationID)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.ActionInventory[0].State != "uncertain" {
				t.Fatalf("failed smoke durable receipt: %+v", receipt.ActionInventory)
			}
		})
	}
}
