// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"strings"
	"testing"
)

func TestLocalStageOfficialPhaseArguments(t *testing.T) {
	v := stageCLIInvocation{Workspace: "/private/capture/repo", Config: "/private/config.json", Override: "/private/override.json", Driver: "/private/driver", UserData: "/private/user-data", Labels: []string{"devcontainer.local_folder=/original", "anchorage-staging=1"}}
	stage, e := v.Arguments("up")
	if e != nil {
		t.Fatal(e)
	}
	joined := strings.Join(stage, " ")
	for _, required := range []string{"--skip-post-create true", "--skip-post-attach true", "--mount-workspace-git-root false", "--mount-git-worktree-common-dir false", "--omit-syntax-directive true"} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing %s: %v", required, stage)
		}
	}
	for _, forbidden := range []string{"--update-remote-user-uid-default", "--default-user-env-probe", "--gpu-availability"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("stage replaced official behavior: %v", stage)
		}
	}
	v.ContainerID = strings.Repeat("a", 64)
	hooks, e := v.Arguments("run-user-commands")
	if e != nil || strings.Contains(strings.Join(hooks, " "), "--skip-post-create") {
		t.Fatalf("hooks suppressed: %v %v", hooks, e)
	}
}
func TestLocalStageSourceMetadataOriginIsNotStringDedup(t *testing.T) {
	source := map[string]any{"postCreateCommand": "same", "remoteUser": "vscode"}
	picked := stageSourceMetadata(source)
	metadata := []map[string]any{{"postCreateCommand": "same"}, picked}
	if e := stageCheckBakedSourceMetadata(source, metadata); e != nil {
		t.Fatal(e)
	}
	if len(metadata) != 2 {
		t.Fatal("distinct origins deduplicated")
	}
	bad := []map[string]any{{"postCreateCommand": "same"}}
	if e := stageCheckBakedSourceMetadata(source, bad); e == nil || !strings.Contains(e.Error(), "source metadata origin") {
		t.Fatalf("string-only origin admitted: %v", e)
	}
}

func TestLocalStageSourceMetadataOriginRequiresActualBakeBranch(t *testing.T) {
	hook := "echo ${localEnv:ANCHORAGE_HOST_NAME}"
	for _, tc := range []struct {
		name   string
		config map[string]any
		baked  bool
	}{
		{"plain image", map[string]any{"image": "example/base", "postCreateCommand": hook}, false},
		{"build", map[string]any{"build": map[string]any{"dockerfile": "Dockerfile"}, "postCreateCommand": hook}, true},
		{"features", map[string]any{"image": "example/base", "features": map[string]any{"example/feature:1": map[string]any{}}, "postCreateCommand": hook}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if stageSourceContributionBaked(tc.config) != tc.baked {
				t.Fatal("wrong official source-origin branch")
			}
			projection := stageSourceMetadata(tc.config)
			if projection["postCreateCommand"] != hook {
				t.Fatal("raw substitution token was resolved in image-label provenance")
			}
			if err := stageCheckBakedSourceMetadata(projection, []map[string]any{projection}); err != nil {
				t.Fatal(err)
			}
			if tc.baked {
				if err := stageCheckBakedSourceMetadata(projection, []map[string]any{{"postCreateCommand": "echo resolved"}}); err == nil {
					t.Fatal("accepted different raw source contribution")
				}
			}
		})
	}
}
