//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func reviewPromotionConsent() LifecycleConsent {
	return LifecycleConsent{Confirmed: true, Volumes: true, Force: true, Quiescence: true}
}

func requireRecoveryStopped(t *testing.T, p *LocalPromotion, path string, result LifecycleResult, err error, reason string) {
	t.Helper()
	if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || result.Status != "partial" || !strings.Contains(err.Error(), reason) {
		t.Fatalf("recovery refusal: result=%+v error=%v; want partial/73 and %q", result, err, reason)
	}
	state := readFakePromotionState(t, path)
	if state.Candidate == nil || state.Volume == nil {
		t.Fatal("refusal deleted candidate resources")
	}
	for _, call := range state.Calls {
		if len(call) > 1 && call[0] == "container" && (call[1] == "start" || call[1] == "rm") {
			t.Fatalf("unsafe recovery action: %v", call)
		}
	}
	if state.Retained == nil || state.Retained["Id"] != p.primaryObservation.ID || state.Retained["Running"] != false {
		t.Fatalf("original not retained stopped: %+v", state.Retained)
	}
}

func TestLocalPromoteRecoveryRefusesOverlappingForeignMounts(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"alias", "parent", "child", "missing", "home_volume_only"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			p, path, _ := fixtureVerifiedPromotion(t)
			state := readFakePromotionState(t, path)
			raw, err := json.Marshal(state.Primary)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &state.Failed); err != nil {
				t.Fatal(err)
			}
			state.Failed["Id"], state.Failed["Name"], state.Failed["Labels"] = strings.Repeat("f", 64), "/foreign", map[string]any{}
			source := p.receipt.OriginalSource
			reason := "resource recovery shared primary mount"
			switch kind {
			case "alias":
				source += "-alias"
				if err := os.Symlink(p.receipt.OriginalSource, source); err != nil {
					t.Fatal(err)
				}
			case "parent":
				source = filepath.Dir(source)
			case "child":
				source = filepath.Join(source, ".devcontainer")
			case "missing":
				source += "-missing"
				reason = "resource recovery bind source unavailable"
			}
			mounts := []map[string]any{{"Type": "bind", "Source": source, "Destination": "/foreign", "RW": true}}
			if kind == "home_volume_only" {
				mounts = []map[string]any{{"Type": "volume", "Name": p.primaryConfig.Home, "Destination": "/home/foreign", "RW": true}}
			}
			state.Failed["Mounts"], state.Failure = mounts, "hooks"
			writeFakePromotionState(t, path, state)
			result, err := p.Apply(context.Background(), reviewPromotionConsent())
			requireRecoveryStopped(t, p, path, result, err, reason)
		})
	}
}

func TestLocalPromoteRecoveryAllowsNonOverlappingOrReadOnlyBinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"sibling_prefix", "readonly_alias"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			p, path, _ := fixtureVerifiedPromotion(t)
			state := readFakePromotionState(t, path)
			raw, _ := json.Marshal(state.Primary)
			if err := json.Unmarshal(raw, &state.Failed); err != nil {
				t.Fatal(err)
			}
			source := p.receipt.OriginalSource + "-sibling"
			rw := true
			if kind == "readonly_alias" {
				rw = false
				if err := os.Symlink(p.receipt.OriginalSource, source); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			state.Failed["Id"], state.Failed["Name"], state.Failed["Labels"] = strings.Repeat("f", 64), "/foreign", map[string]any{}
			state.Failed["Mounts"] = []map[string]any{{"Type": "bind", "Source": source, "Destination": "/foreign", "RW": rw}}
			state.Failure = "hooks"
			writeFakePromotionState(t, path, state)
			result, err := p.Apply(context.Background(), reviewPromotionConsent())
			state = readFakePromotionState(t, path)
			if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "primary container hooks failed") || result.Status != "partial" || state.Primary == nil || state.Primary["Id"] != p.primaryObservation.ID || state.Primary["Running"] != true {
				t.Fatalf("safe bind prevented bounded recovery: %+v %v primary=%v", result, err, state.Primary)
			}
		})
	}
}

func TestLocalPromoteRecoveryRechecksBothConfigurationsBetweenActions(t *testing.T) {
	t.Parallel()
	for _, after := range []string{"stop_failed", "retain_failed", "restore_name"} {
		for _, persisted := range []bool{false, true} {
			name := after + "/temporary"
			if persisted {
				name = after + "/persisted"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				p, path, _ := fixtureVerifiedPromotion(t)
				state := readFakePromotionState(t, path)
				state.Failure, state.MutationAfter, state.MutationPath = "hooks", after, p.configurationPath
				reason := "promotion immutable configuration changed"
				if persisted {
					state.MutationPath = filepath.Join(p.store.operationPath(p.receipt.OperationID), "promote", "devcontainer.json")
					reason = "promotion persistent configuration identity changed"
				}
				writeFakePromotionState(t, path, state)
				result, err := p.Apply(context.Background(), reviewPromotionConsent())
				if err == nil || exitcode.FromError(err) != exitcode.PartialFailure || result.Status != "partial" || !strings.Contains(err.Error(), reason) {
					t.Fatalf("configuration drift: %+v %v; want %s", result, err, reason)
				}
				state = readFakePromotionState(t, path)
				if state.Candidate == nil || state.Volume == nil {
					t.Fatal("configuration drift deleted candidate resources")
				}
				for _, call := range state.Calls {
					if len(call) > 1 && call[0] == "container" && (call[1] == "start" || call[1] == "rm") {
						t.Fatalf("continued recovery after config drift: %v", call)
					}
				}
			})
		}
	}
}

func TestLocalPromoteClientFailureRequiresFreshRunningObservation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"hooks_client_stopped", "hooks_client_unreachable", "smoke_client_stopped", "smoke_client_unreachable"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			p, path, _ := fixtureVerifiedPromotion(t)
			state := readFakePromotionState(t, path)
			state.Failure = mode
			writeFakePromotionState(t, path, state)
			result, err := p.Apply(context.Background(), reviewPromotionConsent())
			reason := "promotion failure replacement not observed running with admitted binding"
			if strings.HasSuffix(mode, "unreachable") {
				reason = "promotion failure replacement observation unavailable"
			}
			requireRecoveryStopped(t, p, path, result, err, reason)
			action := "container_hooks"
			if strings.HasPrefix(mode, "smoke") {
				action = "smoke"
			}
			found := false
			for _, effect := range result.Effects {
				if effect.Action == action {
					found = true
					if effect.State != "uncertain" {
						t.Fatalf("client failure recorded as known: %+v", effect)
					}
				}
			}
			if !found {
				t.Fatalf("missing uncertain %s effect", action)
			}
		})
	}
}
