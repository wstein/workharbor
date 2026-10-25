// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const publicClaudePath = ".claude/settings.json"
const publicClaudeDefault = "{\n  \"attribution\": {\n    \"sessionUrl\": false\n  }\n}\n"

func claudePublicEnvelope(t *testing.T, got result, code int, reason string) map[string]any {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit=%d want %d: %s", got.code, code, got.err)
	}
	doc, err := jsonUnmarshal(got.stdout)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileSchema(t).Validate(doc); err != nil {
		t.Fatalf("invalid settings envelope: %v", err)
	}
	envelope := decodeEnvelope(t, got.stdout)
	if envelope["ok"] != (code == 0) || envelope["exit_code"] != float64(code) {
		t.Fatalf("wrong status envelope: %v", envelope)
	}
	if code != 0 {
		name := "conflict"
		if code == 70 {
			name = "needs_human"
		}
		body := envelope["error"].(map[string]any)
		if body["code"] != name || !strings.Contains(body["message"].(string), reason) || !strings.Contains(got.err, reason) {
			t.Fatalf("wrong typed refusal or reason: %v / %s", body, got.err)
		}
	}
	value, _ := envelope["result"].(map[string]any)
	return value
}

func claudePublicAction(t *testing.T, value map[string]any, diagnostics, action, parentAction string, applied, dry bool) map[string]any {
	t.Helper()
	if value["dry_run"] != dry {
		t.Fatalf("dry_run=%v", value["dry_run"])
	}
	var found map[string]any
	count, parents := 0, 0
	for _, raw := range value["actions"].([]any) {
		a := raw.(map[string]any)
		if dry && a["applied"] != false {
			t.Fatalf("dry-run action applied: %v", a)
		}
		if a["path"] == publicClaudePath {
			count++
			found = a
			if a["action"] != action || a["applied"] != applied {
				t.Fatalf("settings action=%v, want %s applied=%v", a, action, applied)
			}
		}
		if a["path"] == ".claude" {
			parents++
			if a["kind"] != "directory" || a["action"] != parentAction {
				t.Fatalf("parent action=%v, want %s", a, parentAction)
			}
		}
	}
	if count != 1 || parents != 1 {
		t.Fatalf("settings rows=%d parent rows=%d", count, parents)
	}
	steps := 0
	for _, raw := range value["next_steps"].([]any) {
		step := raw.(string)
		if strings.Contains(step, "attribution.sessionUrl: false") && strings.Contains(step, "--overwrite .claude/settings.json") && strings.Contains(step, "whole file") {
			steps++
		}
	}
	wantSteps := 0
	if action == "left_unchanged" {
		wantSteps = 1
	}
	if steps != wantSteps || !strings.Contains(diagnostics, action+" file "+publicClaudePath) {
		t.Fatalf("settings remediation/output: steps=%d stderr=%s", steps, diagnostics)
	}
	if warnings, ok := value["agents_block_warnings"].([]any); !ok || len(warnings) != 0 {
		t.Fatalf("settings must add no agent warning: %v", value["agents_block_warnings"])
	}
	return found
}

func claudeUntouched(t *testing.T, target string, before os.FileInfo) {
	t.Helper()
	after, err := os.Stat(target)
	if before == nil {
		if !os.IsNotExist(err) {
			t.Fatalf("absent settings path changed: %v", err)
		}
		return
	}
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		t.Fatalf("untouched settings identity/time/mode changed: %v", err)
	}
}

func TestInitPublicClaudeSettingsBaselineAndNoop(t *testing.T) {
	for _, kind := range []string{"absent_parent", "existing_parent", "identical"} {
		t.Run(kind, func(t *testing.T) {
			dir := initFixture(t)
			parent := filepath.Join(dir, ".claude")
			target := filepath.Join(dir, publicClaudePath)
			var fileBefore, parentBefore os.FileInfo
			if kind != "absent_parent" {
				if err := os.Mkdir(parent, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "identical" {
				if err := os.WriteFile(target, []byte(publicClaudeDefault), 0644); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(1700000000, 0)
				if err := os.Chtimes(target, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				var err error
				fileBefore, err = os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
			}
			if kind != "absent_parent" {
				var err error
				parentBefore, err = os.Stat(parent)
				if err != nil {
					t.Fatal(err)
				}
			}
			rootBefore, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"init", dir, "--non-interactive", "--yes", "--json"}
			action, parentAction := "create", "reuse"
			if kind == "identical" {
				action = "skip_existing"
			}
			if kind == "absent_parent" {
				parentAction = "create"
			}
			dry := run(t, nil, append(args, "--dry-run")...)
			preview := claudePublicAction(t, claudePublicEnvelope(t, dry, 0, ""), dry.err, action, parentAction, false, true)
			claudeUntouched(t, target, fileBefore)
			claudeUntouched(t, parent, parentBefore)
			claudeUntouched(t, dir, rootBefore)
			got := run(t, nil, args...)
			written := claudePublicAction(t, claudePublicEnvelope(t, got, 0, ""), got.err, action, parentAction, action == "create", false)
			if preview["diff"] != written["diff"] || preview["diff_truncated"] != written["diff_truncated"] {
				t.Fatal("written settings diff differs from preview")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != publicClaudeDefault {
				t.Fatalf("settings bytes=%q: %v", data, err)
			}
			info, err := os.Stat(target)
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatalf("settings mode must be 0644: %v / %v", info, err)
			}
			if kind == "identical" {
				claudeUntouched(t, target, fileBefore)
			}
			fileBefore, parentBefore = info, nil
			parentBefore, err = os.Stat(parent)
			if err != nil {
				t.Fatal(err)
			}
			rootBefore, err = os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			got = run(t, nil, args...)
			value := claudePublicEnvelope(t, got, 0, "")
			claudePublicAction(t, value, got.err, "skip_existing", "reuse", false, false)
			for _, raw := range value["actions"].([]any) {
				if raw.(map[string]any)["applied"] != false {
					t.Fatal("rerun applied an action")
				}
			}
			claudeUntouched(t, target, fileBefore)
			claudeUntouched(t, parent, parentBefore)
			claudeUntouched(t, dir, rootBefore)
		})
	}
	for _, overwrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("appeared_after_preview/overwrite=%v", overwrite), func(t *testing.T) {
			dir := initFixture(t)
			parent := filepath.Join(dir, ".claude")
			if err := os.Mkdir(parent, 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, publicClaudePath)
			var concurrentInfo os.FileInfo
			var out bytes.Buffer
			diagnostics := &initPreviewWriter{before: func() {
				if err := os.WriteFile(target, []byte("concurrent owner settings\n"), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				concurrentInfo, err = os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
			}}
			args := []string{"init", dir, "--non-interactive", "--yes", "--json"}
			if overwrite {
				args = append(args, "--overwrite="+publicClaudePath)
			}
			code := runWithContext(context.Background(), &out, diagnostics, args...)
			value := claudePublicEnvelope(t, result{code, out.String(), diagnostics.String()}, 68, "init target changed since preview or is unsafe")
			for _, raw := range value["actions"].([]any) {
				if raw.(map[string]any)["applied"] != false {
					t.Fatal("stale proposal applied an action")
				}
			}
			claudeUntouched(t, target, concurrentInfo)
			if data, err := os.ReadFile(target); err != nil || string(data) != "concurrent owner settings\n" {
				t.Fatalf("concurrent settings changed: %q / %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
				t.Fatalf("stale preview wrote manifest: %v", err)
			}
		})
	}
}

func TestInitPublicClaudeSettingsLeftUnchanged(t *testing.T) {
	for _, old := range []string{`{"owner":true}`, "invalid json\n", "", strings.Repeat("x", 64*1024+1)} {
		for _, overwrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("bytes=%d/overwrite=%v", len(old), overwrite), func(t *testing.T) {
				dir := initFixture(t)
				parent := filepath.Join(dir, ".claude")
				if err := os.Mkdir(parent, 0755); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(dir, publicClaudePath)
				if err := os.WriteFile(target, []byte(old), 0600); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(1700000000, 0)
				if err := os.Chtimes(target, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"init", dir, "--non-interactive", "--yes", "--json"}
				action, expected := "left_unchanged", old
				if overwrite {
					args = append(args, "--overwrite="+publicClaudePath)
					action, expected = "overwrite", publicClaudeDefault
				}
				dry := run(t, nil, append(args, "--dry-run")...)
				preview := claudePublicAction(t, claudePublicEnvelope(t, dry, 0, ""), dry.err, action, "reuse", false, true)
				claudeUntouched(t, target, before)
				if data, err := os.ReadFile(target); err != nil || string(data) != old {
					t.Fatalf("dry-run altered settings: %v", err)
				}
				got := run(t, nil, args...)
				actual := claudePublicAction(t, claudePublicEnvelope(t, got, 0, ""), got.err, action, "reuse", overwrite, false)
				if preview["diff"] != actual["diff"] || preview["diff_truncated"] != actual["diff_truncated"] {
					t.Fatal("overwrite preview changed")
				}
				if data, err := os.ReadFile(target); err != nil || string(data) != expected {
					t.Fatalf("wrong settings outcome: %v", err)
				}
				if !overwrite {
					claudeUntouched(t, target, before)
				}
				before, err = os.Stat(target)
				if err != nil || before.Mode().Perm() != 0600 {
					t.Fatalf("private settings mode changed: %v / %v", before, err)
				}
				got = run(t, nil, args...)
				if overwrite {
					action = "skip_existing"
				}
				claudePublicAction(t, claudePublicEnvelope(t, got, 0, ""), got.err, action, "reuse", false, false)
				claudeUntouched(t, target, before)
			})
		}
	}
	t.Run("overwrite_requires_yes", func(t *testing.T) {
		dir := initFixture(t)
		got := run(t, nil, "init", dir, "--non-interactive", "--overwrite="+publicClaudePath, "--json")
		claudePublicEnvelope(t, got, 70, "--non-interactive requires --yes (or --dry-run)")
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("missing-consent refusal changed workspace: %v", err)
		}
	})
}
