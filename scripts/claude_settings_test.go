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
