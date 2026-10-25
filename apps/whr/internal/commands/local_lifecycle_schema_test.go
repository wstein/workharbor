// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

func lifecycleText(v string) *string { return &v }
func lifecycleFixtureResult(status string) localdocker.LifecycleResult {
	digest := "sha256:" + strings.Repeat("a", 64)
	primary := localdocker.StageResource{Kind: "container", Name: lifecycleText("primary"), ID: lifecycleText(strings.Repeat("b", 64)), DaemonID: "daemon"}
	binding := localdocker.StageRepositoryBinding{SourcePath: "/project", GitDir: "/project/.git", CommonDir: "/project/.git", ObjectFormat: "sha1", OriginalHeadOID: strings.Repeat("c", 40), BindingDigest: digest}
	target := "refs/workharbor/stage/" + strings.Repeat("d", 32) + "/" + strings.Repeat("e", 64) + "/head"
	return localdocker.LifecycleResult{
		Status: status, Phase: "preflight", RecoveryID: lifecycleText("daemon/" + strings.Repeat("d", 32)), Fingerprint: &digest, Primary: &primary,
		Candidate:    &localdocker.StageResource{Kind: "container", Name: lifecycleText("whr-anchorage-staging"), ID: nil, DaemonID: "daemon"},
		Home:         &localdocker.StageResource{Kind: "volume", Name: lifecycleText("whr-stage-home-" + strings.Repeat("d", 32)), ID: nil, DaemonID: "daemon"},
		Verification: &localdocker.StageVerification{Policy: "smoke", Status: "passed", BindingMode: "cooperative_quiescence", Quiescence: "caller_acknowledged", PreInputDigest: digest, PostInputDigest: &digest, ImageID: digest, InputDigest: digest, ObservedAt: "2026-10-25T10:00:00Z", OutputDigest: nil},
		Effects: []localdocker.StageEffect{
			{Action: "create_container", State: "planned", Target: localdocker.StageTarget{Resource: &primary}},
			{Action: "import_objects", State: "planned", Target: localdocker.StageTarget{Objects: &localdocker.StageGitObjectsTarget{Kind: "git_objects", Repository: binding, ExportPath: "/private/export.git", ObjectManifestDigest: digest, ObjectCount: 3}}},
			{Action: "create_result_refs", State: "planned", Target: localdocker.StageTarget{Refs: &localdocker.StageGitRefsTarget{Kind: "git_refs", Repository: binding, Creates: []localdocker.StageRefCreate{{Name: target, OID: strings.Repeat("c", 40), ExpectedAbsent: true}}}}},
			{Action: "publish_candidate_output", State: "planned", Target: localdocker.StageTarget{Output: &localdocker.StageCloneOutputTarget{Kind: "clone_output", ClonePath: "/private/clone", RelativePath: ".devcontainer/host.env", Digest: digest, ProducerOperationID: strings.Repeat("d", 32)}}},
		},
		Recovery:     []localdocker.StageRecoveryStep{{Action: "inspect_retained", Target: nil, Instruction: "Inspect the retained candidate and clone."}},
		Repositories: []localdocker.StageRepository{{SourcePath: "/project", ClonePath: "/private/clone", CloneOwnerUID: 1000, CloneRootMode: 0700, ContainerWorkspacePath: "/workspaces/project", HeadOID: strings.Repeat("c", 40), ObjectFormat: "sha1", Dirty: true, UncommittedRetained: true}},
		ResultRefs:   []localdocker.StageResultRef{{RepositoryPath: "/project", ExportRef: "HEAD", TargetRef: target, OID: strings.Repeat("c", 40), State: "planned"}},
	}
}

func lifecycleDocument(t *testing.T, command string, r localdocker.LifecycleResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"schema_version": 1, "command": command, "ok": true, "exit_code": 0, "result": result}
}
func lifecycleObject(v any) map[string]any      { return v.(map[string]any) }
func lifecycleItem(v any, i int) map[string]any { return lifecycleObject(v.([]any)[i]) }

func TestLocalLifecycleSchemaClosesResultsAndTargets(t *testing.T) {
	schema := compileSchema(t)
	for _, command := range []string{"workspace.stage", "workspace.promote"} {
		if err := schema.Validate(lifecycleDocument(t, command, lifecycleFixtureResult("preview"))); err != nil {
			t.Fatal(err)
		}
	}
	unknownImage := lifecycleFixtureResult("preview")
	unknownImage.Verification = nil
	if err := schema.Validate(lifecycleDocument(t, "workspace.stage", unknownImage)); err != nil {
		t.Fatalf("preview before image identity is known: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unknown_result", func(r map[string]any) { r["secret"] = "forbidden" }},
		{"missing_nullable", func(r map[string]any) { delete(r, "fingerprint") }},
		{"null_effects", func(r map[string]any) { r["effects"] = nil }},
		{"null_refs", func(r map[string]any) { r["result_refs"] = nil }},
		{"unknown_status", func(r map[string]any) { r["status"] = "running" }},
		{"unknown_phase", func(r map[string]any) { r["phase"] = "remote_operation" }},
		{"reason_without_unverified", func(r map[string]any) { r["unverified_reason"] = "missing_smoke" }},
		{"unverified_without_reason", func(r map[string]any) { r["status"] = "staged_unverified" }},
		{"resource_unknown", func(r map[string]any) { lifecycleObject(r["primary"])["environment"] = "secret" }},
		{"short_container_id", func(r map[string]any) { lifecycleObject(r["primary"])["id"] = "abcd" }},
		{"unknown_verification_image", func(r map[string]any) { lifecycleObject(r["verification"])["image_id"] = "" }},
		{"wrong_repository_oid", func(r map[string]any) { lifecycleItem(r["repositories"], 0)["object_format"] = "sha256" }},
		{"public_clone_mode", func(r map[string]any) { lifecycleItem(r["repositories"], 0)["clone_root_mode"] = 0755 }},
		{"bad_result_namespace", func(r map[string]any) { lifecycleItem(r["result_refs"], 0)["target_ref"] = "refs/heads/main" }},
		{"result_state", func(r map[string]any) { lifecycleItem(r["result_refs"], 0)["state"] = "verified" }},
		{"missing_binding", func(r map[string]any) {
			delete(lifecycleObject(lifecycleItem(r["effects"], 1)["target"]), "repository")
		}},
		{"wrong_binding_oid", func(r map[string]any) {
			lifecycleObject(lifecycleObject(lifecycleItem(r["effects"], 1)["target"])["repository"])["object_format"] = "sha256"
		}},
		{"excess_objects", func(r map[string]any) {
			lifecycleObject(lifecycleItem(r["effects"], 1)["target"])["object_count"] = 1000001
		}},
		{"ref_overwrite", func(r map[string]any) {
			lifecycleItem(lifecycleObject(lifecycleItem(r["effects"], 2)["target"])["creates"], 0)["expected_absent"] = false
		}},
		{"wrong_action_target", func(r map[string]any) { lifecycleItem(r["effects"], 1)["target"] = r["primary"] }},
		{"bad_output_producer", func(r map[string]any) {
			lifecycleObject(lifecycleItem(r["effects"], 3)["target"])["producer_operation_id"] = "remote"
		}},
		{"unknown_recovery", func(r map[string]any) { lifecycleItem(r["recovery"], 0)["action"] = "delete_unconditionally" }},
		{"preview_applied", func(r map[string]any) { lifecycleItem(r["effects"], 0)["state"] = "observed" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := lifecycleDocument(t, "workspace.stage", lifecycleFixtureResult("preview"))
			tc.mutate(lifecycleObject(doc["result"]))
			if err := schema.Validate(doc); err == nil {
				t.Fatalf("schema accepted %s", tc.name)
			}
		})
	}
}

func TestLocalLifecycleSchemaBindsOutcomeAndErrorInventory(t *testing.T) {
	schema := compileSchema(t)
	for _, command := range []string{"workspace.stage", "workspace.promote"} {
		doc := lifecycleDocument(t, command, lifecycleFixtureResult("partial"))
		doc["ok"], doc["exit_code"], doc["error"] = false, 73, map[string]any{"code": "partial_failure", "message": "retained local effects"}
		if err := schema.Validate(doc); err != nil {
			t.Fatal(err)
		}
		delete(doc, "result")
		if err := schema.Validate(doc); err == nil {
			t.Fatal("partial failure omitted retained inventory")
		}
	}
	r := lifecycleFixtureResult("staged_unverified")
	r.UnverifiedReason = lifecycleText("missing_smoke")
	doc := lifecycleDocument(t, "workspace.stage", r)
	if err := schema.Validate(doc); err == nil {
		t.Fatal("unverified candidate reported success")
	}
	doc["ok"], doc["exit_code"], doc["error"] = false, 73, map[string]any{"code": "partial_failure", "message": "candidate is not verified"}
	if err := schema.Validate(doc); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(lifecycleDocument(t, "workspace.promote", lifecycleFixtureResult("staged_verified"))); err == nil {
		t.Fatal("promote accepted a stage-only success")
	}
}
