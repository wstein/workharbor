package scripts_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestClaudeForgePermissions checks the tracked configuration, not native
// Claude Code enforcement. Actual session usage remains a separate check.
func TestClaudeForgePermissions(t *testing.T) {
	data, err := os.ReadFile("../.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"gh issue", "gh project", "gh api graphql"} {
		t.Run(family, func(t *testing.T) {
			if !slices.Contains(settings.Permissions.Deny, "Bash("+family+":*)") {
				t.Errorf("missing explicit deny for %s", family)
			}
			for _, rule := range settings.Permissions.Allow {
				if rule == "Bash("+family+")" || strings.HasPrefix(rule, "Bash("+family+" ") || strings.HasPrefix(rule, "Bash("+family+":") {
					t.Errorf("conflicting allow rule %s", rule)
				}
			}
		})
	}
	for _, rule := range []string{
		"Bash(gh api repos/wstein/workharbor/code-scanning/alerts)",
		"Bash(gh api repos/wstein/workharbor/dependabot/alerts)",
		"Bash(gh api repos/wstein/workharbor/rulesets)",
		"Bash(scripts/board-snapshot.sh)",
		"Bash(scripts/board-snapshot.sh --refresh)",
		"Bash(scripts/board-snapshot.sh card:*)",
		"Bash(scripts/board-snapshot.sh queue:*)",
	} {
		if !slices.Contains(settings.Permissions.Allow, rule) {
			t.Errorf("missing retained route %s", rule)
		}
	}
}

// This is a static configuration contract, not a Claude permission evaluator.
// No fixture opens a home directory or invokes a credential command.
func TestClaudePortablePermissions(t *testing.T) {
	data, err := os.ReadFile("../.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct{ Allow, Deny []string }
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for _, rule := range settings.Permissions.Allow {
		if strings.HasPrefix(rule, "Read(") {
			t.Errorf("shared settings must not grant external checkout reads: %s", rule)
		}
	}
	for _, rule := range append(slices.Clone(settings.Permissions.Allow), settings.Permissions.Deny...) {
		for _, binding := range []string{"/Users/", "/home/", "/root/", "werner"} {
			if strings.Contains(rule, binding) {
				t.Errorf("machine-specific binding in %s", rule)
			}
		}
	}
	for _, suffix := range []string{".ssh", ".config/whr", ".config/gh", "Library/Keychains"} {
		for _, anchor := range []string{"~/", "//**/"} {
			rule := "Read(" + anchor + suffix + "/**)"
			if !slices.Contains(settings.Permissions.Deny, rule) {
				t.Errorf("missing sensitive file deny %s", rule)
			}
		}
		for _, command := range []string{"cat", "grep -r", "rg"} {
			rule := "Bash(" + command + " */" + suffix + "*)"
			if !slices.Contains(settings.Permissions.Deny, rule) {
				t.Errorf("missing sensitive command deny %s", rule)
			}
		}
	}
	for _, rule := range []string{
		"Read(//Library/Keychains/**)", "Bash(cat /Library/Keychains:*)",
		"Bash(grep -r /Library/Keychains:*)", "Bash(rg /Library/Keychains:*)",
		"Bash(security:*)", "Bash(gh auth:*)", "Bash(git credential:*)",
		"Bash(sudo:*)", "Bash(launchctl:*)", "Bash(git push:*)",
		"Bash(git merge:*)", "Bash(git tag:*)", "Bash(gh pr merge:*)",
		"Bash(gh release create:*)", "Bash(gh release edit:*)",
		"Bash(gh release delete:*)", "Bash(gh release upload:*)",
		"Bash(gh api repos/wstein/workharbor/secret-scanning:*)",
	} {
		if !slices.Contains(settings.Permissions.Deny, rule) {
			t.Errorf("missing retained command/system deny %s", rule)
		}
	}
}
