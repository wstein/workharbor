// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"strings"
	"testing"
	"time"
)

func TestLocalPromoteRealCLILookupCreatesDistinctPrimary(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if err != nil || result.Status != "promoted" {
		t.Fatalf("real label-lookup model must create a distinct primary: %+v %v", result, err)
	}
	state := readFakePromotionState(t, path)
	if state.Primary == nil || state.Primary["Id"] == p.candidateObservation.ID {
		t.Fatal("candidate was reused as primary")
	}
	lookup := false
	for _, call := range state.Calls {
		if len(call) > 0 && call[0] == "ps" {
			lookup = true
		}
	}
	if !lookup {
		t.Fatal("official-CLI label lookup was not exercised")
	}
}

func TestLocalPromoteFailedReplacementRetainsOriginalWritableLayer(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	state := readFakePromotionState(t, path)
	state.Failure = "up"
	writeFakePromotionState(t, path, state)
	result, err := p.Apply(context.Background(), LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || err == nil || !strings.Contains(err.Error(), "primary resource phase failed") {
		t.Fatalf("replacement refusal: %+v %v", result, err)
	}
	state = readFakePromotionState(t, path)
	if state.Retained == nil || state.Retained["Id"] != p.primaryObservation.ID || state.Retained["Running"] != false {
		t.Fatalf("original stopped container/writable layer lost: %+v", state.Retained)
	}
	for _, call := range state.Calls {
		if len(call) > 1 && call[0] == "container" && call[1] == "rm" && call[len(call)-1] == p.primaryObservation.ID {
			t.Fatal("original removed before replacement verification")
		}
	}
	if len(result.Recovery) == 0 {
		t.Fatal("missing exact recovery guidance")
	}
}

func TestLocalPromoteGuardReasonsAndProtectedEffects(t *testing.T) {
	for _, tc := range []struct {
		failure, reason string
		health, smoke   bool
	}{
		{"still_running", "promotion stopped primary observation changed", false, true},
		{"wrong_reply_image", "official promotion up identity or configuration response invalid", false, true},
		{"candidate_reply", "official promotion up identity or configuration response invalid", false, true},
		{"missing_verification", "promotion missing_smoke", true, false},
		{"unhealthy", "promotion primary healthcheck failed", true, false},
		{"late_home_drift", "promotion candidate home creation observation changed before deletion", false, true},
	} {
		t.Run(tc.failure, func(t *testing.T) {
			p, path, _ := fixtureVerifiedPromotionPolicy(t, tc.health, tc.smoke, nil)
			state := readFakePromotionState(t, path)
			state.Failure = tc.failure
			writeFakePromotionState(t, path, state)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := p.Apply(ctx, LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true})
			if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), tc.reason) || result.Status != "partial" {
				t.Fatalf("guard %s: %+v %v", tc.failure, result, err)
			}
			if tc.failure == "late_home_drift" {
				state = readFakePromotionState(t, path)
				if state.Volume == nil {
					t.Fatal("late replaced home deleted")
				}
				for _, call := range state.Calls {
					if len(call) > 1 && call[0] == "volume" && call[1] == "rm" {
						t.Fatal("unsafe volume removal attempted")
					}
				}
			}
		})
	}
}

func TestLocalPromotePreEffectErrorsKeepExitAndReason(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   exitcode.Code
		reason string
	}{
		{context.DeadlineExceeded, exitcode.Timeout, "local promote preflight deadline exceeded"},
		{context.Canceled, exitcode.Interrupted, "local promote preflight interrupted"},
		{stageConflict("retained input changed"), exitcode.Conflict, "retained input changed"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			p := &LocalPromotion{result: LifecycleResult{Phase: "preflight", Effects: []StageEffect{}}}
			result, err := p.partial("before resource change", tc.err)
			if exitcode.FromError(err) != tc.code || !strings.Contains(err.Error(), tc.reason) || result.Status != "refused" {
				t.Fatalf("pre-effect mapping: %+v %v", result, err)
			}
			if len(result.Recovery) != 1 || !strings.Contains(result.Recovery[0].Instruction, "No Docker or Git effects were attempted") {
				t.Fatalf("pre-effect report omitted what did not run: %+v", result.Recovery)
			}
		})
	}
}

func TestLocalPromoteLookupCollisionRefusesBeforeJournalOrPrimaryEffects(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	state := readFakePromotionState(t, path)
	raw, _ := json.Marshal(state.Primary)
	var other map[string]any
	if err := json.Unmarshal(raw, &other); err != nil {
		t.Fatal(err)
	}
	other["Id"] = strings.Repeat("f", 64)
	other["Name"] = "/foreign-match"
	labels := other["Labels"].(map[string]any)
	labels["workharbor.stage.operation"] = p.receipt.OperationID
	labels["workharbor.promote.operation"] = p.receipt.OperationID
	state.Retained = other
	writeFakePromotionState(t, path, state)
	assertPromotionRefusal(t, p, path, LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}, exitcode.Conflict, "promotion replacement labels already match an existing container")
}

func TestLocalPromotePreviewNamesRetainedOriginal(t *testing.T) {
	p, _, _ := fixtureVerifiedPromotion(t)
	for _, action := range []string{"rename_primary", "remove_primary"} {
		found := false
		for _, effect := range p.Preview().Effects {
			if effect.Action != action {
				continue
			}
			found = true
			if effect.Target.Resource == nil || effect.Target.Resource.Name == nil || *effect.Target.Resource.Name != p.retainedPrimaryName() || effect.Target.Resource.ID == nil || *effect.Target.Resource.ID != p.primaryObservation.ID {
				t.Fatalf("%s lacks exact retained name/ID: %+v", action, effect.Target.Resource)
			}
		}
		if !found {
			t.Fatalf("missing effect %s", action)
		}
	}
}
