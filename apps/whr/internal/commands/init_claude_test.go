// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitPublicClaudeSettingsBaselineAndNoop(t *testing.T) {
	dir := initFixture(t)
	for round := 0; round < 2; round++ {
		got := run(t, nil, "init", dir, "--non-interactive", "--yes", "--json")
		if got.code != 0 {
			t.Fatal(got.err)
		}
		doc, err := jsonUnmarshal(got.stdout)
		if err != nil {
			t.Fatal(err)
		}
		if err := compileSchema(t).Validate(doc); err != nil {
			t.Fatal(err)
		}
		result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
		found := 0
		for _, raw := range result["actions"].([]any) {
			a := raw.(map[string]any)
			if round == 1 && a["applied"] != false {
				t.Fatalf("rerun mutated %v", a)
			}
			if a["path"] == ".claude/settings.json" {
				found++
				want := "create"
				if round == 1 {
					want = "skip_existing"
				}
				if a["action"] != want {
					t.Fatalf("settings=%v", a)
				}
			}
		}
		if found != 1 {
			t.Fatalf("settings rows=%d", found)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".claude/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{\n  \"attribution\": {\n    \"sessionUrl\": false\n  }\n}\n" {
		t.Fatalf("settings=%q", raw)
	}
}

func TestInitPublicClaudeSettingsLeftUnchanged(t *testing.T) {
	dir := initFixture(t)
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, ".claude/settings.json")
	if err := os.WriteFile(target, []byte("invalid json\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := run(t, nil, "init", dir, "--non-interactive", "--yes", "--json")
	if got.code != 0 {
		t.Fatal(got.err)
	}
	result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
	found := 0
	for _, raw := range result["actions"].([]any) {
		a := raw.(map[string]any)
		if a["path"] == ".claude/settings.json" {
			found++
			if a["action"] != "left_unchanged" || a["applied"] != false {
				t.Fatalf("settings=%v", a)
			}
		}
	}
	if found != 1 || !strings.Contains(got.err, "left_unchanged file .claude/settings.json") {
		t.Fatal("missing unchanged outcome")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "invalid json\n" {
		t.Fatalf("preservation=%q, %v", raw, err)
	}
}
