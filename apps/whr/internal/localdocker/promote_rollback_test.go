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
)

func TestLocalPromoteKnownHookFailureRestoresOriginalAndRetainsReplacement(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	state := readFakePromotionState(t, path)
	state.Failure = "hooks"
	writeFakePromotionState(t, path, state)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "primary container hooks failed") {
		t.Fatalf("failure: %+v %v", result, err)
	}
	state = readFakePromotionState(t, path)
	if state.Primary == nil || state.Primary["Id"] != p.primaryObservation.ID || state.Primary["Running"] != true || state.Primary["Name"] != p.primaryObservation.Name {
		t.Fatalf("original not restored: %+v", state.Primary)
	}
	if state.Failed == nil || state.Failed["Id"] != strings.Repeat("e", 64) || state.Failed["Running"] != false || state.Failed["Name"] != "/whr-failed-primary-"+p.receipt.OperationID {
		t.Fatalf("failed writable layer not retained: %+v", state.Failed)
	}
	if state.Candidate == nil || state.Volume == nil {
		t.Fatal("rollback removed candidate resources")
	}
	for _, call := range state.Calls {
		if len(call) > 1 && call[0] == "container" && call[1] == "rm" {
			t.Fatalf("rollback deleted writable layer: %v", call)
		}
	}
	if len(result.Recovery) == 0 || !strings.Contains(result.Recovery[0].Instruction, "original container restored") {
		t.Fatalf("rollback not disclosed: %+v", result.Recovery)
	}
}

func TestLocalPromoteRollbackRefusesUncertainOrSharedResources(t *testing.T) {
	for _, tc := range []struct{ failure, reason string }{
		{"hooks_shared", "resource recovery shared primary mount"},
		{"hooks_occupied", "resource recovery name occupied"},
		{"hooks_stop_failure", "phase command failed: stop_failed_primary"},
		{"hooks_start_failure", "phase command failed: restart_original_primary"},
		{"hooks_signal", "primary container hooks failed"},
		{"up", "primary resource phase failed"},
	} {
		t.Run(tc.failure, func(t *testing.T) {
			p, path, _ := fixtureVerifiedPromotion(t)
			state := readFakePromotionState(t, path)
			state.Failure = tc.failure
			writeFakePromotionState(t, path, state)
			result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
			if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), tc.reason) || result.Status != "partial" {
				t.Fatalf("refusal: %+v %v", result, err)
			}
			state = readFakePromotionState(t, path)
			if state.Candidate == nil || state.Volume == nil {
				t.Fatal("refusal removed candidate resources")
			}
			for _, call := range state.Calls {
				if len(call) > 1 && call[0] == "container" && call[1] == "rm" {
					t.Fatalf("refusal deleted writable layer: %v", call)
				}
				if tc.failure != "hooks_start_failure" && len(call) > 1 && call[0] == "container" && call[1] == "start" {
					t.Fatalf("unsafe restart: %v", call)
				}
			}
			if tc.failure == "hooks_start_failure" && (state.Primary == nil || state.Primary["Id"] != p.primaryObservation.ID || state.Primary["Running"] != false) {
				t.Fatalf("failed start wrongly reported running: %+v", state.Primary)
			}
			if tc.failure == "hooks_stop_failure" && (state.Retained == nil || state.Retained["Name"] != "/"+p.retainedPrimaryName()) {
				t.Fatal("stop uncertainty renamed original")
			}
		})
	}
}

func TestLocalPromoteKnownUnhealthyReplacementRestoresOriginal(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotionPolicy(t, true, false, nil)
	state := readFakePromotionState(t, path)
	state.Failure = "unhealthy"
	writeFakePromotionState(t, path, state)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "promotion primary healthcheck failed") {
		t.Fatalf("health failure: %+v %v", result, err)
	}
	state = readFakePromotionState(t, path)
	if state.Primary == nil || state.Primary["Id"] != p.primaryObservation.ID || state.Primary["Running"] != true || state.Failed == nil || state.Failed["Running"] != false {
		t.Fatalf("unhealthy recovery not observed: primary=%v failed=%v", state.Primary, state.Failed)
	}
}

func TestLocalPromotionCanonicalizesAliasedTemporaryRoot(t *testing.T) {
	p, _, ports := fixtureVerifiedPromotion(t)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	physical := filepath.Join(parent, "physical")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	fresh, err := (Client{}).preparePromotion(context.Background(), PromoteOptions{Target: p.primaryObservation.ID}, ports)
	if err != nil {
		t.Fatalf("aliased TMPDIR: %v", err)
	}
	defer fresh.Close()
	resolved, err := filepath.EvalSymlinks(fresh.root)
	if err != nil || resolved != fresh.root {
		t.Fatalf("promotion root is not physical: %q %q %v", fresh.root, resolved, err)
	}
	result, err := fresh.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err != nil || result.Status != "promoted" {
		t.Fatalf("aliased TMPDIR promotion: %+v %v", result, err)
	}
}

func TestLocalPromoteRechecksCleanupAfterOriginalRemoval(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	state := readFakePromotionState(t, path)
	state.Failure = "retained_rm_late_shared"
	writeFakePromotionState(t, path, state)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "mounted by another container") {
		t.Fatalf("late cleanup sharing: %+v %v", result, err)
	}
	state = readFakePromotionState(t, path)
	if state.Candidate == nil || state.Volume == nil {
		t.Fatal("late sharing removed candidate resources")
	}
}
