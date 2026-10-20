// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitPublicAgentsActionsAndWarningsValidate(t *testing.T) {
	body := "foreign workflow\n"
	block := func(version int) string {
		return fmt.Sprintf("<!-- whr:agents-block begin v=%d sha256=%x -->\n%s<!-- whr:agents-block end -->\n", version, sha256.Sum256([]byte(body)), body)
	}
	cases := []struct {
		name, body, action, warning string
		edit                        bool
		code                        int
	}{
		{"same version foreign intact", block(1), "left_unchanged", "agents_block.different_template", false, 0},
		{"higher intact replacement", block(2), "replace_block", "", true, 0},
		{"missing", "owner\n", "left_unchanged", "agents_block.missing", false, 0},
		{"append", "owner\r\n", "append_block", "", true, 0},
		{"malformed", "<!-- whr:agents-block end -->\n", "left_unchanged", "agents_block.malformed", false, 0},
		{"malformed requested", "<!-- whr:agents-block end -->\n", "", "", true, 68},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := initFixture(t)
			if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(tt.body), 0644); err != nil {
				t.Fatal(err)
			}
			args := []string{"init", dir, "--non-interactive", "--yes", "--json"}
			if tt.edit {
				args = append(args, "--overwrite=AGENTS.md")
			}
			got := run(t, nil, args...)
			if got.code != tt.code {
				t.Fatalf("code %d: %s", got.code, got.err)
			}
			doc, err := jsonUnmarshal(got.stdout)
			if err != nil {
				t.Fatal(err)
			}
			if err := compileSchema(t).Validate(doc); err != nil {
				t.Fatalf("invalid current output contract: %v", err)
			}
			if tt.code != 0 {
				return
			}
			result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
			warnings, ok := result["agents_block_warnings"].([]any)
			if !ok {
				t.Fatal("missing warnings array")
			}
			if tt.warning == "" {
				if len(warnings) != 0 {
					t.Fatalf("warnings %v", warnings)
				}
			} else if len(warnings) != 1 || warnings[0].(map[string]any)["code"] != tt.warning {
				t.Fatalf("warnings %v", warnings)
			}
			found := 0
			for _, raw := range result["actions"].([]any) {
				a := raw.(map[string]any)
				if a["path"] != "AGENTS.md" {
					continue
				}
				found++
				if a["action"] != tt.action || a["applied"] != (tt.action == "append_block" || tt.action == "replace_block") {
					t.Fatalf("action %v", a)
				}
			}
			if found != 1 {
				t.Fatalf("AGENTS actions %d", found)
			}
			if tt.warning != "" && !strings.Contains(got.err, tt.warning) {
				t.Fatal("warning not printed")
			}
		})
	}
}
func TestInitPublicNoAgentsBlockConflictAndNoEntry(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		dir := initFixture(t)
		args := []string{"init", dir, "--dry-run", "--no-agents-block", "--json"}
		if conflict {
			args = append(args, "--overwrite=AGENTS.md")
		}
		got := run(t, nil, args...)
		if conflict {
			if got.code != 64 {
				t.Fatalf("conflict %d", got.code)
			}
			continue
		}
		if got.code != 0 {
			t.Fatalf("optout failed %s", got.err)
		}
		result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
		for _, raw := range result["actions"].([]any) {
			if raw.(map[string]any)["path"] == "AGENTS.md" {
				t.Fatal("disabled block proposed")
			}
		}
	}
}
