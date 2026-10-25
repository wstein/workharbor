// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"strings"
	"testing"
)

func TestLocalPromoteSourceOriginsUseBoundRawContribution(t *testing.T) {
	hook := "echo ${localEnv:ANCHORAGE_CONTAINER_NAME}"
	for _, tc := range []struct {
		name                  string
		baked, mismatch, keep bool
	}{
		{"independent_image_identical_text", false, false, true},
		{"built_exact_raw_stage_contribution", true, false, false},
		{"built_mismatched_contribution_refuses", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := map[string]any{"image": "example:local", "postCreateCommand": hook, "mounts": []any{"type=volume,source=original-home,target=/home/vscode"}}
			if tc.baked {
				source["build"] = map[string]any{"dockerfile": "Dockerfile"}
			}
			bound := stageSourceMetadata(source)
			if tc.baked {
				bound["mounts"] = []any{"type=volume,source=whr-stage-home-bound,target=/home/vscode"}
			}
			imageMetadata := stageCopyConfig(bound)
			if tc.mismatch {
				imageMetadata["postCreateCommand"] = "echo substituted"
			}
			p := &LocalPromotion{sourceConfig: source, sourceMetadata: bound, image: stageImageDetails{Metadata: []map[string]any{imageMetadata}}, primaryConfig: stagePrimaryConfiguration{Source: "/original/project", Workspace: "/workspaces/project", Home: "anchorage-home-primary", HomeDestination: "/home/vscode", Name: "primary", RemoteUser: "vscode", Mounts: []any{}, RunArgs: []any{}, ContainerEnv: map[string]any{}, RemoteEnv: map[string]any{}}, receipt: stageReceipt{Verification: &StageVerification{ImageID: "sha256:" + strings.Repeat("d", 64)}}}
			config, err := p.boundConfiguration()
			if tc.mismatch {
				if exitcode.FromError(err) != exitcode.Conflict || err == nil || !strings.Contains(err.Error(), "source metadata origin does not match admitted full contribution") {
					t.Fatalf("origin refusal: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, kept := config["postCreateCommand"]
			if kept != tc.keep {
				t.Fatalf("source origin duplicated or dropped: keep=%v want=%v config=%v", kept, tc.keep, config)
			}
			if tc.keep && config["postCreateCommand"] != hook {
				t.Fatal("raw substitution token changed")
			}
		})
	}
}
