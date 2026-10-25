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

func TestLocalStageReviewRejectsChangedSmokeBeforeExecution(t *testing.T) {
	client, opts, ports, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	stage, err := client.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	state := readFakeStageState(t, path)
	// Change E immediately after the hook phase's last source/capture inventory,
	// as the primary metadata response is returned. No hostile script is run.
	state.MutateCapturedSmokePath = filepath.Join(stage.capture.Path, ".devcontainer", "smoke-container.sh")
	data, _ := json.Marshal(state)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
	if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), "trusted smoke bytes changed") {
		t.Errorf("changed smoke not refused at execution boundary: %+v %v", result, err)
	}
	state = readFakeStageState(t, path)
	if state.SmokeStdin != "" {
		t.Fatalf("untrusted smoke reached Docker exec: %q", state.SmokeStdin)
	}
	for _, call := range state.Calls {
		if strings.Contains(strings.Join(call, " "), "bash -s") {
			t.Fatalf("untrusted smoke command started: %q", call)
		}
	}
}

func TestLocalStageReviewImageAndTmpfsMountPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, volume, mountType string
		refused                 bool
	}{
		{"uncovered_volume", "/cache", "", true},
		{"nested_volume", "/workspaces/Anchorage/cache", "", true},
		{"covered_home", "/home/vscode", "", false},
		{"covered_workspace", "/workspaces/Anchorage", "", false},
		{"tmpfs", "", "tmpfs", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, opts, ports, path := fixtureStageLifecycle(t, false)
			opts.Smoke = true
			if tc.volume != "" {
				state := readFakeStageState(t, path)
				state.ImageVolumes = []string{tc.volume}
				writeStageReviewState(t, path, state)
			}
			if tc.mountType != "" {
				state := readFakeStageState(t, path)
				mounts := state.Primary["Mounts"].([]any)
				state.Primary["Mounts"] = append(mounts, map[string]any{"Type": tc.mountType, "Destination": "/cache", "RW": true})
				data, _ := json.Marshal(state)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			stage, err := client.prepareStage(context.Background(), opts, ports)
			if tc.mountType != "" {
				if exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), "unsupported planned mount type") {
					t.Fatalf("tmpfs not refused before allocation: %v", err)
				}
				state := readFakeStageState(t, path)
				if state.Volume != nil || state.Candidate != nil {
					t.Fatalf("mount refusal allocated resources: %+v", state)
				}
				assertStageReviewNoEffects(t, state)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
			if tc.refused {
				reason := "image volume requires explicit planned mount"
				if tc.mountType != "" {
					reason = "unsupported planned mount type"
				}
				if exitcode.FromError(err) != exitcode.PartialFailure || !strings.Contains(err.Error(), reason) || result.Status == "staged_verified" {
					t.Fatalf("unsupported replacement mount verified: %+v %v", result, err)
				}
			} else if err != nil || result.Status != "staged_verified" {
				t.Fatalf("covered image volume refused: %+v %v", result, err)
			}
		})
	}
}
