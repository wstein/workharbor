package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rulesetFake puts a gh on PATH that logs its argv and answers GET with the
// committed previous payload, so show/plan see a diff against the active one.
func rulesetFake(t *testing.T) (path, log string) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}
	bin := t.TempDir()
	log = filepath.Join(bin, "gh.log")
	live, err := os.ReadFile(filepath.Join("..", ".github", "rulesets", "main.previous.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "live.json"), live, 0o600); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$*\" >> \"" + log + "\"\ncat \"" + bin + "/live.json\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil { //nolint:gosec // a fake executable
		t.Fatal(err)
	}
	return bin, log
}

func runRuleset(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", append([]string{"ruleset-apply.sh"}, args...)...) //nolint:gosec // fixed script, test args
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin", "HOME=" + t.TempDir()}
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func ghCalls(t *testing.T, log string) string {
	t.Helper()
	b, err := os.ReadFile(log) //nolint:gosec // test temp log
	if err != nil {
		return ""
	}
	return string(b)
}

func TestRulesetApplyDryRunNeverMutates(t *testing.T) {
	bin, log := rulesetFake(t)
	for _, args := range [][]string{{"show"}, {"plan"}, {"apply"}, {"apply", "--dry-run"}, {"plan", "--stage", "active"}} {
		out, err := runRuleset(t, bin, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	calls := ghCalls(t, log)
	if strings.Contains(calls, "-X") || strings.Contains(calls, "PUT") || strings.Contains(calls, "POST") {
		t.Fatalf("mutating gh call without --apply:\n%s", calls)
	}
	if !strings.Contains(calls, "api repos/wstein/workharbor/rulesets/24335774") {
		t.Fatalf("no read of the live ruleset:\n%s", calls)
	}
	out, _ := runRuleset(t, bin, "show")
	if !strings.Contains(out, "pull_request") {
		t.Fatalf("show does not print the diff:\n%s", out)
	}
}

func TestRulesetApplyRefusesWithoutTerminal(t *testing.T) {
	bin, log := rulesetFake(t)
	out, err := runRuleset(t, bin, "apply", "--apply")
	if err == nil || !strings.Contains(out, "without a terminal") {
		t.Fatalf("want refusal, got err=%v\n%s", err, out)
	}
	if strings.Contains(ghCalls(t, log), "-X") {
		t.Fatal("mutating call without a terminal")
	}
	if _, err := runRuleset(t, bin, "plan", "--apply"); err == nil {
		t.Fatal("plan --apply must be refused")
	}
}

func loadPayload(t *testing.T, stage string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".github", "rulesets", "main."+stage+".json")) //nolint:gosec // fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", stage, err)
	}
	return m
}

func ruleTypes(m map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, r := range m["rules"].([]any) {
		rm := r.(map[string]any)
		p, _ := rm["parameters"].(map[string]any)
		out[rm["type"].(string)] = p
	}
	return out
}

func TestRulesetPayloads(t *testing.T) {
	for stage, enf := range map[string]string{"active": "active", "previous": "active"} {
		m := loadPayload(t, stage)
		if m["enforcement"] != enf || m["id"].(float64) != 24335774 {
			t.Fatalf("%s: enforcement/id wrong: %v %v", stage, m["enforcement"], m["id"])
		}
		if b, ok := m["bypass_actors"].([]any); !ok || len(b) != 0 {
			t.Fatalf("%s: bypass_actors must be empty: %v", stage, m["bypass_actors"])
		}
		rt := ruleTypes(m)
		for _, want := range []string{"deletion", "required_linear_history", "non_fast_forward"} {
			if _, ok := rt[want]; !ok {
				t.Fatalf("%s: missing rule %s", stage, want)
			}
		}
		if stage == "previous" {
			if len(rt) != 3 {
				t.Fatalf("previous must hold the 3 old rules, got %d", len(rt))
			}
			continue
		}
		pr := rt["pull_request"]
		if pr == nil || pr["required_approving_review_count"].(float64) != 0 || pr["require_code_owner_review"] != false {
			t.Fatalf("%s: bad pull_request rule: %v", stage, pr)
		}
		if mm := pr["allowed_merge_methods"].([]any); len(mm) != 1 || mm[0] != "rebase" {
			t.Fatalf("%s: merge methods: %v", stage, mm)
		}
		got := map[string]bool{}
		for _, c := range rt["required_status_checks"]["required_status_checks"].([]any) {
			cm := c.(map[string]any)
			got[cm["context"].(string)] = true
			if id, ok := cm["integration_id"].(float64); (cm["context"] == "gate") != ok || (ok && id != 15368) {
				t.Fatalf("%s: integration_id only on gate, 15368: %v", stage, cm)
			}
		}
		for _, want := range []string{"gate", "commits", "secrets"} {
			if !got[want] {
				t.Fatalf("%s: missing required check %s", stage, want)
			}
		}
	}
}

func TestRulesetRestoreDryRunAndNoTerminal(t *testing.T) {
	bin, log := rulesetFake(t)
	backup := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(backup, []byte(`{"id":24335774,"name":"main","target":"branch","enforcement":"active","conditions":{},"bypass_actors":[],"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runRuleset(t, bin, "restore", backup); err != nil || !strings.Contains(out, "dry run") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if out, err := runRuleset(t, bin, "restore", backup, "--apply"); err == nil || !strings.Contains(out, "without a terminal") {
		t.Fatalf("want refusal: %v\n%s", err, out)
	}
	if strings.Contains(ghCalls(t, log), "-X") {
		t.Fatal("restore mutated")
	}
}

func TestRulesetApplyDryRunWritesNoBackup(t *testing.T) {
	bin, _ := rulesetFake(t)
	dir := filepath.Join(t.TempDir(), "state")
	if out, err := runRuleset(t, bin, "apply", "--backup-dir", dir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("dry run created the backup dir")
	}
}

// apply calls the same save_backup as the backup subcommand; a fixed date makes
// the second run hit the same name, which must be refused, not overwritten.
func TestRulesetBackupCreatesAndNeverOverwrites(t *testing.T) {
	bin, log := rulesetFake(t)
	date := "#!/bin/sh\necho 20261008T100000Z\n"
	if err := os.WriteFile(filepath.Join(bin, "date"), []byte(date), 0o755); err != nil { //nolint:gosec // a fake executable
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "state")
	out, err := runRuleset(t, bin, "backup", "--backup-dir", dir)
	if err != nil || !strings.Contains(out, "backup saved: ") {
		t.Fatalf("%v\n%s", err, out)
	}
	f := filepath.Join(dir, "ruleset-24335774-20261008T100000Z.json")
	b, err := os.ReadFile(f) //nolint:gosec // test temp file
	if err != nil || !strings.Contains(string(b), "required_linear_history") {
		t.Fatalf("backup content: %v %s", err, b)
	}
	if err := os.WriteFile(f, []byte("keep"), 0o600); err != nil { //nolint:gosec // test temp file
		t.Fatal(err)
	}
	if out, err := runRuleset(t, bin, "backup", "--backup-dir", dir); err == nil || !strings.Contains(out, "never overwritten") {
		t.Fatalf("want refusal: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(f); string(b) != "keep" { //nolint:gosec // test temp file
		t.Fatal("backup was overwritten")
	}
	if strings.Contains(ghCalls(t, log), "-X") {
		t.Fatal("backup mutated")
	}
}
