package scripts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
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

// TestClaudeSharedAllowExcludesBoardWrites is a negative guard: the dispatcher's
// board write grant is applied per dispatch session, never as a shared allow
// rule. Read-only queue and card routes stay allowed (see
// TestClaudeForgePermissions). This is a static check of the tracked rules, not
// a Claude permission evaluator.
func TestClaudeSharedAllowExcludesBoardWrites(t *testing.T) {
	data, err := os.ReadFile("../.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct{ Allow []string }
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"move", "ready", "session", "priority", "add"} {
		for _, prefix := range []string{"scripts/board-snapshot.sh", "./scripts/board-snapshot.sh"} {
			command := prefix + " " + verb + " 1 x"
			t.Run(command, func(t *testing.T) {
				for _, rule := range settings.Permissions.Allow {
					if allowRuleCoversBash(rule, command) {
						t.Errorf("shared allow rule %s covers board write %q", rule, command)
					}
				}
			})
		}
	}
}

// allowRuleCoversBash reports whether a Claude Code allow rule would cover the
// Bash command: a bare "Bash", "Bash(*)", a "prefix:*" rule, a "*" wildcard
// pattern or an exact match.
func allowRuleCoversBash(rule, command string) bool {
	if rule == "Bash" {
		return true
	}
	spec, ok := strings.CutPrefix(rule, "Bash(")
	if !ok {
		return false
	}
	spec, ok = strings.CutSuffix(spec, ")")
	if !ok {
		return false
	}
	if prefix, legacy := strings.CutSuffix(spec, ":*"); legacy {
		return command == prefix || strings.HasPrefix(command, prefix+" ")
	}
	return wildcardMatch(spec, command)
}

// wildcardMatch matches pattern against s where "*" matches any run of
// characters, "/" included.
func wildcardMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(s, part)
		if i < 0 {
			return false
		}
		s = s[i+len(part):]
	}
	return strings.HasSuffix(s, last)
}

func TestAllowRuleCoversBash(t *testing.T) {
	const move = "scripts/board-snapshot.sh move 1 Done"
	for _, tc := range []struct {
		rule string
		want bool
	}{
		{"Bash", true},
		{"Bash(*)", true},
		{"Bash(scripts/*)", true},
		{"Bash(scripts/board-snapshot.sh:*)", true},
		{"Bash(scripts/board-snapshot.sh move:*)", true},
		{"Bash(scripts/board-snapshot.sh *)", true},
		{move, false},
		{"Bash(" + move + ")", true},
		{"Bash(scripts/board-snapshot.sh)", false},
		{"Bash(scripts/board-snapshot.sh queue:*)", false},
		{"Bash(scripts/board-snapshot.sh card:*)", false},
		{"Bash(scripts/board-snapshot.sh --refresh)", false},
		{"Grep", false},
	} {
		if got := allowRuleCoversBash(tc.rule, move); got != tc.want {
			t.Errorf("allowRuleCoversBash(%q) = %v, want %v", tc.rule, got, tc.want)
		}
	}
}

func TestPersonalSettingsAndPythonCachesIgnored(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	ignore, err := os.ReadFile("../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), ignore, 0o600); err != nil { //nolint:gosec // fixed file in a private synthetic repository
		t.Fatal(err)
	}
	if out, err := gittest.Git(t.Context(), home, repo, nil, "init").CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, out)
	}
	for _, tc := range []struct {
		path    string
		ignored bool
	}{
		{".claude/settings.local.json", true},
		{"nested/.claude/settings.local.json", true},
		{".claude/settings.json", false},
		{"nested/.claude/settings.json", false},
		{"__pycache__/module.cpython-313.pyc", true},
		{"nested/__pycache__/module.cpython-313.pyc", true},
		{"module.pyc", false},
		{"nested/module.pyc", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			cmd := gittest.Git(t.Context(), home, repo, nil, "check-ignore", "--no-index", tc.path)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if tc.ignored {
				if err != nil || strings.TrimSpace(string(out)) != tc.path {
					t.Fatalf("want ignored: %v: stdout=%q stderr=%q", err, out, stderr.String())
				}
			} else if err == nil || len(out) != 0 {
				t.Fatalf("non-ignored file must remain trackable: %v: stdout=%q stderr=%q", err, out, stderr.String())
			} else if exit, ok := err.(interface{ ExitCode() int }); !ok || exit.ExitCode() != 1 {
				t.Fatalf("check-ignore failed unexpectedly: %v: stderr=%q", err, stderr.String())
			}
		})
	}
}
