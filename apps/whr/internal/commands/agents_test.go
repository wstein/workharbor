// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
)

func TestInitPublicAgentsActionsAndWarningsValidate(t *testing.T) {
	dir := initFixture(t)
	snapshot, err := scaffold.Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := scaffold.Build(snapshot, scaffold.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	current := shipped.Files()["AGENTS.md"]
	var shippedVersion int
	if _, err := fmt.Sscanf(string(current), "<!-- whr:agents-block begin v=%d", &shippedVersion); err != nil || shippedVersion < 1 {
		t.Fatalf("invalid shipped marker: %v", err)
	}
	block := func(version int) []byte {
		body := "foreign workflow\n"
		return []byte(fmt.Sprintf("<!-- whr:agents-block begin v=%d sha256=%x -->\n%s<!-- whr:agents-block end -->\n", version, sha256.Sum256([]byte(body)), body))
	}
	wrap := func(body []byte) []byte {
		return bytes.Join([][]byte{[]byte("\xef\xbb\xbfowner\r\n"), body, []byte("tail\r\n")}, nil)
	}
	begin := bytes.SplitAfter(current, []byte("\n"))[0]
	cases := []struct {
		name            string
		data            []byte
		action, warning string
		written         []byte
		refused         bool
		mode            os.FileMode
	}{
		{"absent", nil, "create", "", current, false, 0644},
		{"empty", []byte{}, "append_block", "missing", append([]byte("\n"), current...), false, 0644},
		{"missing", []byte("owner"), "append_block", "missing", append([]byte("owner\n\n"), current...), false, 0644},
		{"CRLF BOM missing", []byte("\xef\xbb\xbfowner\r\n"), "append_block", "missing", append([]byte("\xef\xbb\xbfowner\r\n\n"), current...), false, 0644},
		{"current", wrap(current), "skip_existing", "", wrap(current), false, 0644},
		{"lower intact", wrap(block(shippedVersion - 1)), "replace_block", "different_template", wrap(current), false, 0644},
		{"same version foreign intact", wrap(block(shippedVersion)), "replace_block", "different_template", wrap(current), false, 0644},
		{"higher intact", wrap(block(shippedVersion + 1)), "replace_block", "different_template", wrap(current), false, 0644},
		{"modified", wrap(bytes.Replace(current, []byte("## Agent workflow"), []byte("## Owner workflow"), 1)), "replace_block", "modified", wrap(current), false, 0644},
		{"lower modified", wrap(bytes.Replace(block(shippedVersion-1), []byte("foreign workflow"), []byte("owner workflow"), 1)), "replace_block", "modified", wrap(current), false, 0644},
		{"higher modified", wrap(bytes.Replace(block(shippedVersion+1), []byte("foreign workflow"), []byte("owner workflow"), 1)), "replace_block", "modified", wrap(current), false, 0644},
		{"oversized", bytes.Repeat([]byte("x"), 64*1024+1), "", "too_large", nil, true, 0644},
		{"oversized malformed", bytes.Repeat([]byte("<!-- whr:agents-block end -->\n"), 3000), "", "too_large", nil, true, 0644},
		{"result oversized", bytes.Repeat([]byte("x"), 64*1024-1), "", "too_large", nil, true, 0644},
		{"duplicate", bytes.Join([][]byte{current, current}, nil), "", "malformed", nil, true, 0644},
		{"unclosed", begin, "", "malformed", nil, true, 0644},
		{"orphan", []byte("<!-- whr:agents-block end -->\n"), "", "malformed", nil, true, 0644},
		{"nested", bytes.Join([][]byte{begin, current}, nil), "", "malformed", nil, true, 0644},
		{"end before begin", bytes.Join([][]byte{[]byte("<!-- whr:agents-block end -->\n"), current}, nil), "", "malformed", nil, true, 0644},
		{"CRLF markers", bytes.ReplaceAll(current, []byte("\n"), []byte("\r\n")), "", "malformed", nil, true, 0644},
		{"indented marker", append([]byte("  "), current...), "", "malformed", nil, true, 0644},
		{"BOM marker", append([]byte("\xef\xbb\xbf"), current...), "", "malformed", nil, true, 0644},
		{"fenced marker", []byte("```\n\t<!-- whr:agents-block unknown -->\n```\n"), "", "malformed", nil, true, 0644},
		{"private missing", []byte("private owner\n"), "append_block", "missing", append([]byte("private owner\n\n"), current...), false, 0600},
		{"private foreign", wrap(block(shippedVersion + 1)), "replace_block", "different_template", wrap(current), false, 0600},
	}
	for _, tt := range cases {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/edit=%v", tt.name, edit), func(t *testing.T) {
				dir := initFixture(t)
				target := filepath.Join(dir, "AGENTS.md")
				if tt.data != nil {
					if err := os.WriteFile(target, tt.data, tt.mode); err != nil {
						t.Fatal(err)
					}
				}
				var originalInfo os.FileInfo
				if tt.data != nil {
					stamp := time.Unix(1700000000, 0)
					if err := os.Chtimes(target, stamp, stamp); err != nil {
						t.Fatal(err)
					}
					originalInfo, err = os.Stat(target)
					if err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"init", dir, "--non-interactive", "--yes", "--json"}
				if edit {
					args = append(args, "--overwrite=AGENTS.md")
				}
				wantAction, wantWarning, wantData := tt.action, "", tt.written
				if !edit && tt.warning != "" {
					wantAction, wantWarning, wantData = "left_unchanged", "agents_block."+tt.warning, tt.data
				}
				assertResult := func(got result, dryRun bool, action string, applied bool) map[string]any {
					t.Helper()
					wantCode := 0
					if edit && tt.refused {
						wantCode = 68
					}
					if got.code != wantCode {
						t.Fatalf("code %d want %d: %s", got.code, wantCode, got.err)
					}
					doc, err := jsonUnmarshal(got.stdout)
					if err != nil {
						t.Fatal(err)
					}
					if err := compileSchema(t).Validate(doc); err != nil {
						t.Fatalf("invalid current output contract: %v", err)
					}
					if wantCode != 0 {
						envelope := decodeEnvelope(t, got.stdout)
						body := envelope["error"].(map[string]any)
						reason := "is " + tt.warning + ";"
						if envelope["ok"] != false || envelope["exit_code"] != float64(68) || body["code"] != "conflict" || !strings.Contains(body["message"].(string), reason) || !strings.Contains(got.err, reason) {
							t.Fatalf("refusal reason/envelope mismatch: %v / %s", envelope, got.err)
						}
						result := envelope["result"].(map[string]any)
						for _, key := range []string{"actions", "agents_block_warnings"} {
							if values, ok := result[key].([]any); !ok || len(values) != 0 {
								t.Fatalf("refusal %s must be an empty array: %v", key, result[key])
							}
						}
						return nil
					}
					result := decodeEnvelope(t, got.stdout)["result"].(map[string]any)
					if result["dry_run"] != dryRun {
						t.Fatalf("dry_run: %v", result["dry_run"])
					}
					warnings, ok := result["agents_block_warnings"].([]any)
					if !ok {
						t.Fatal("missing always-present warnings array")
					}
					if wantWarning == "" {
						if len(warnings) != 0 {
							t.Fatalf("warnings %v", warnings)
						}
					} else if len(warnings) != 1 || warnings[0].(map[string]any)["code"] != wantWarning || warnings[0].(map[string]any)["path"] != "AGENTS.md" || !strings.Contains(got.err, wantWarning) {
						t.Fatalf("warning or diagnostic mismatch: %v / %s", warnings, got.err)
					}
					var agentAction map[string]any
					found := 0
					for _, raw := range result["actions"].([]any) {
						a := raw.(map[string]any)
						if a["path"] == "AGENTS.md" {
							found++
							agentAction = a
							if a["action"] != action || a["applied"] != applied {
								t.Fatalf("action %v want %s applied=%v", a, action, applied)
							}
						}
					}
					if found != 1 {
						t.Fatalf("AGENTS actions %d", found)
					}
					return agentAction
				}
				assertOriginal := func() {
					t.Helper()
					data, err := os.ReadFile(target)
					if tt.data == nil {
						if !os.IsNotExist(err) {
							t.Fatalf("absent AGENTS.md changed: %v", err)
						}
					} else {
						if err != nil || !bytes.Equal(data, tt.data) {
							t.Fatalf("original AGENTS.md changed: %v", err)
						}
						info, err := os.Stat(target)
						if err != nil || !os.SameFile(originalInfo, info) || !originalInfo.ModTime().Equal(info.ModTime()) || originalInfo.Mode() != info.Mode() {
							t.Fatalf("untouched AGENTS.md identity, time or mode changed: %v", err)
						}
					}
					if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
						t.Fatalf("dry run/refusal wrote manifest: %v", err)
					}
				}
				preview := assertResult(run(t, nil, append(args, "--dry-run")...), true, wantAction, false)
				assertOriginal()
				mutates := wantAction == "create" || wantAction == "append_block" || wantAction == "replace_block"
				actual := assertResult(run(t, nil, args...), false, wantAction, mutates)
				if edit && tt.refused {
					assertOriginal()
					return
				}
				if preview["diff"] != actual["diff"] || preview["diff_truncated"] != actual["diff_truncated"] {
					t.Fatal("applied diff differs from dry-run preview")
				}
				data, err := os.ReadFile(target)
				if err != nil || !bytes.Equal(data, wantData) {
					t.Fatalf("written bytes differ from specified outcome: %v", err)
				}
				info, err := os.Stat(target)
				if err != nil || info.Mode().Perm() != tt.mode {
					t.Fatalf("AGENTS.md mode must remain %04o: %v / %v", tt.mode, info, err)
				}
				if mutates {
					wantAction = "skip_existing"
				}
				stamp := time.Unix(1700000000, 0)
				if err := os.Chtimes(target, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				fileBefore, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(dir)
				if err != nil {
					t.Fatal(err)
				}
				assertResult(run(t, nil, args...), false, wantAction, false)
				after, err := os.Stat(dir)
				if err != nil || before.ModTime() != after.ModTime() {
					t.Fatalf("no-op rerun changed directory: %v", err)
				}
				fileAfter, err := os.Stat(target)
				if err != nil || !os.SameFile(fileBefore, fileAfter) || !fileBefore.ModTime().Equal(fileAfter.ModTime()) || fileBefore.Mode() != fileAfter.Mode() {
					t.Fatalf("rerun changed AGENTS.md identity, time or mode: %v", err)
				}
				data, err = os.ReadFile(target)
				if err != nil || !bytes.Equal(data, wantData) {
					t.Fatalf("rerun changed AGENTS.md: %v", err)
				}
			})
		}
	}
}
func TestInitPublicNoAgentsBlockConflictAndNoEntry(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		dir := initFixture(t)
		target := filepath.Join(dir, "AGENTS.md")
		original := []byte("<!-- whr:agents-block malformed -->\r\n")
		if err := os.WriteFile(target, original, 0644); err != nil {
			t.Fatal(err)
		}
		args := []string{"init", dir, "--dry-run", "--no-agents-block", "--json"}
		if conflict {
			args = append(args, "--overwrite=AGENTS.md")
		}
		got := run(t, nil, args...)
		if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, original) {
			t.Fatalf("opt-out changed existing AGENTS.md: %v", err)
		}
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
		if warnings, ok := result["agents_block_warnings"].([]any); !ok || len(warnings) != 0 {
			t.Fatalf("opt-out warning array: %v", result["agents_block_warnings"])
		}
		for _, raw := range result["actions"].([]any) {
			if raw.(map[string]any)["path"] == "AGENTS.md" {
				t.Fatal("disabled block proposed")
			}
		}
	}
}
