// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func writeStageReviewState(t *testing.T, path string, state fakeStageState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func assertStageReviewNoEffects(t *testing.T, state fakeStageState) {
	t.Helper()
	for _, call := range state.Calls {
		if len(call) > 0 && (call[0] == "run" || call[0] == "exec" || call[0] == "volume" && len(call) > 1 && call[1] == "create") {
			t.Fatalf("unexpected Docker effect: %q", call)
		}
	}
}
func TestLocalStageReviewFlowRefusalGuards(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		alter        func(*fakeStageState)
	}{
		{"primary_stopped", "primary changed during stage", func(s *fakeStageState) { s.StopPrimaryOnUp = true }},
		{"foreign_operation", "candidate identity does not match durable intent", func(s *fakeStageState) { s.ForeignOperation = true }},
		{"up_returns_primary", "official up identity or configuration response invalid", func(s *fakeStageState) { s.UpReturnsPrimary = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, opts, p, path := fixtureStageLifecycle(t, false)
			opts.Smoke = true
			state := readFakeStageState(t, path)
			tc.alter(&state)
			writeStageReviewState(t, path, state)
			stage, err := c.prepareStage(context.Background(), opts, p)
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), tc.reason) || result.Status != "partial" {
				t.Fatalf("guard missing: %+v %v", result, err)
			}
			state = readFakeStageState(t, path)
			if state.SmokeStdin != "" {
				t.Fatal("refused candidate ran smoke")
			}
		})
	}
}
func TestLocalStageReviewUnhealthyCandidateFlow(t *testing.T) {
	c, opts, p, path := fixtureStageLifecycle(t, true)
	state := readFakeStageState(t, path)
	state.HealthStatus = "unhealthy"
	writeStageReviewState(t, path, state)
	stage, err := c.prepareStage(context.Background(), opts, p)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stage.Apply(ctx, LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "candidate healthcheck failed") || result.Status != "staged_unverified" || result.UnverifiedReason == nil || *result.UnverifiedReason != "failed_health" {
		t.Fatalf("health failure lost: %+v %v", result, err)
	}
	receipt, err := stage.store.loadReceipt(stage.operationID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "staged_unverified" || receipt.UnverifiedReason == nil || *receipt.UnverifiedReason != "failed_health" {
		t.Fatalf("health failure not durable: %+v", receipt)
	}
}
func TestLocalStageReviewCompletedSmokeFailureFlow(t *testing.T) {
	for _, code := range []int{1, 124, 125, 126, 127} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			c, opts, p, path := fixtureStageLifecycle(t, false)
			opts.Smoke = true
			state := readFakeStageState(t, path)
			state.SmokeExit = code
			writeStageReviewState(t, path, state)
			stage, err := c.prepareStage(context.Background(), opts, p)
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "smoke") {
				t.Fatalf("smoke exit admitted: %+v %v", result, err)
			}
			if code < 125 {
				if result.Status != "staged_unverified" || result.UnverifiedReason == nil || *result.UnverifiedReason != "failed_smoke" || !strings.Contains(err.Error(), "exit status ") {
					t.Fatalf("completed smoke wrongly uncertain: %+v %v", result, err)
				}
			} else if result.Status != "partial" {
				t.Fatalf("Docker client failure was marked completed: %+v %v", result, err)
			}
			want := "observed"
			if code >= 125 {
				want = "uncertain"
			}
			seen := false
			for _, effect := range result.Effects {
				if effect.Action == "smoke" {
					seen = true
					if effect.State != want {
						t.Fatalf("smoke state=%s want %s", effect.State, want)
					}
				}
			}
			if !seen {
				t.Fatal("missing smoke effect")
			}
			receipt, err := stage.store.loadReceipt(stage.operationID)
			if err != nil {
				t.Fatal(err)
			}
			if code < 125 && (receipt.State != "staged_unverified" || receipt.UnverifiedReason == nil || *receipt.UnverifiedReason != "failed_smoke") {
				t.Fatalf("smoke failure not durable: %+v", receipt)
			}
			if readFakeStageState(t, path).SmokeStdin == "" {
				t.Fatal("failure fixture did not run smoke")
			}
		})
	}
}
func TestLocalStageReviewInterruptedHealthIsPartial(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "interrupt", true: "deadline"}[deadline], func(t *testing.T) {
			stage := fixtureStageReviewAttempt(t, "candidate_hooks", "container_hooks")
			stage.receipt.ActionInventory[0].State = "observed"
			if err := stage.store.Advance("candidate_hooks", "container_hooks", "observed"); err != nil {
				t.Fatal(err)
			}
			if err := stage.store.Advance("verifying", "", "observed"); err != nil {
				t.Fatal(err)
			}
			if err := stage.store.saveReceipt(stage.receipt, false); err != nil {
				t.Fatal(err)
			}
			var ctx context.Context
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}
			defer cancel()
			result, err := stage.stageUnverified(ctx, "failed_health", "sha256:"+strings.Repeat("d", 64), "sha256:"+strings.Repeat("b", 64), ctx.Err())
			if exitcode.FromError(err) != exitcode.PartialFailure || result.Status != "partial" || result.UnverifiedReason != nil || !strings.Contains(err.Error(), map[bool]string{false: "interrupt", true: "deadline"}[deadline]) {
				t.Fatalf("interrupted health recorded as terminal failure: %+v %v", result, err)
			}
			rows, err := stage.store.Records()
			if err != nil {
				t.Fatal(err)
			}
			if rows[len(rows)-1].State != "verifying" {
				t.Fatalf("health decision invented: %+v", rows[len(rows)-1])
			}
		})
	}
}
func TestLocalStageReviewAnonymousImageVolumeRefused(t *testing.T) {
	c, opts, p, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	state := readFakeStageState(t, path)
	// The primary covers the image VOLUME with a named mount, but the candidate's
	// config does not declare it. Real Docker creates an anonymous candidate volume.
	state.ImageVolumes = []string{"/cache"}
	state.Primary["Mounts"] = append(state.Primary["Mounts"].([]any), map[string]any{"Type": "volume", "Name": "primary-cache", "Source": "/var/lib/docker/volumes/primary-cache/_data", "Destination": "/cache", "RW": true})
	writeStageReviewState(t, path, state)
	stage, err := c.prepareStage(context.Background(), opts, p)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "candidate volume mount not in declared plan") || result.Status != "partial" {
		t.Fatalf("anonymous image volume admitted: %+v %v", result, err)
	}
	state = readFakeStageState(t, path)
	found := false
	for _, raw := range state.Candidate["Mounts"].([]any) {
		m := raw.(map[string]any)
		if m["Name"] == "anonymous-0" {
			found = true
		}
	}
	if !found {
		t.Fatal("fake did not model Docker anonymous volume")
	}
}
