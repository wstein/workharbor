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
	for _, rule := range append([]string{
		"Bash(gh api repos/wstein/workharbor/code-scanning/alerts)",
		"Bash(gh api repos/wstein/workharbor/dependabot/alerts)",
		"Bash(gh api repos/wstein/workharbor/rulesets)",
	}, boardReadOnlyRoutes...) {
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
		for _, prefix := range []string{
			"scripts/board-snapshot.sh", "./scripts/board-snapshot.sh",
			"bash scripts/board-snapshot.sh", "sh scripts/board-snapshot.sh",
			"scripts/board-snapshot.sh --refresh",
		} {
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
	// Any rule that mentions the script must be one of the known read-only routes.
	for _, rule := range settings.Permissions.Allow {
		if strings.Contains(rule, "board-snapshot") && !slices.Contains(boardReadOnlyRoutes, rule) {
			t.Errorf("allow rule %s mentions board-snapshot but is not a known read-only route: review it, then add it to the shared boardReadOnlyRoutes list", rule)
		}
	}
	// Structural check: every Bash allow rule starts with a literal, known
	// first word, so no wrapper, interpreter, path or wildcard-first rule can
	// reach the board script by another spelling.
	for _, rule := range settings.Permissions.Allow {
		if !allowRuleHasKnownFirstWord(rule) {
			t.Errorf("allow rule %s must be a tool rule from allowedToolRules, an exact Bash rule with a known first word and an argument, or be listed in allowedWildcardRules; a new wildcard rule needs an edit to this security-relevant test and an Opus review", rule)
		}
	}
}

// boardReadOnlyRoutes are the only shared allow rules that may mention the
// board script.
var boardReadOnlyRoutes = []string{
	"Bash(scripts/board-snapshot.sh)",
	"Bash(scripts/board-snapshot.sh --refresh)",
	"Bash(scripts/board-snapshot.sh card:*)",
	"Bash(scripts/board-snapshot.sh queue:*)",
}

// allowedBashFirstWords are the literal first words the committed Bash allow
// rules start with. Add one only after review.
var allowedBashFirstWords = []string{"git", "make", "gh", "df", "container", "scripts/board-snapshot.sh"}

// allowedToolRules are the bare non-Bash tool rules the committed file uses.
var allowedToolRules = []string{"Grep", "Glob", "WebSearch"}

// allowedWildcardRules are the only reviewed wildcard Bash rules in the shared
// file. A new one needs an edit to this security-relevant test and an Opus
// review.
var allowedWildcardRules = []string{
	"Bash(gh run list:*)",
	"Bash(gh run view:*)",
	"Bash(gh release list:*)",
	"Bash(scripts/board-snapshot.sh card:*)",
	"Bash(scripts/board-snapshot.sh queue:*)",
}

// allowRuleHasKnownFirstWord is a static guard on the rule text, not a
// permission evaluator. A bare tool rule must be in allowedToolRules. A Bash
// rule containing "*" or ending in ":*" must be in allowedWildcardRules. Any
// other Bash rule is exact: it must start with a literal first word from
// allowedBashFirstWords and, except for the bare board script, carry at least
// one argument word (so Bash(git) and Bash(make) fail). This keeps wrappers,
// interpreters, paths, quoting and open-ended wildcard rules out of the shared
// file. It does not prove that a permitted command is harmless: an exact rule
// for git, make, gh or container still allows whatever that command does with
// those exact arguments.
func allowRuleHasKnownFirstWord(rule string) bool {
	if !strings.HasPrefix(rule, "Bash") {
		return slices.Contains(allowedToolRules, rule)
	}
	spec, ok := strings.CutPrefix(rule, "Bash(")
	if !ok || !strings.HasSuffix(rule, ")") {
		return false
	}
	spec = strings.TrimSuffix(spec, ")")
	if strings.Contains(spec, "*") {
		return slices.Contains(allowedWildcardRules, rule)
	}
	words := strings.Split(spec, " ")
	if !slices.Contains(allowedBashFirstWords, words[0]) {
		return false
	}
	return len(words) >= 2 || words[0] == "scripts/board-snapshot.sh"
}

func TestClaudeAllowRuleHasKnownFirstWord(t *testing.T) {
	for _, rule := range []string{
		"Bash", "Bash(*)", "Bash(env:*)", "Bash(env *)", "Bash(bash -c *)", "Bash(sh -c:*)",
		"Bash(zsh:*)", "Bash(exec:*)", "Bash(source:*)", "Bash(. scripts/*)", "Bash(/*)",
		"Bash(/*:*)", "Bash(WHR_BOARD_SNAPSHOT=* scripts/*)", "Bash(*=* *)", "Bash('scripts/*)",
		`Bash("scripts/*)`, "Bash(scripts/./*)", "Bash(scripts//*)",
		"Bash(scripts/board-snap* move 42 Done)", "Bash(scripts/board*:*)", "Bash(*/board-snapshot.sh:*)",
		"Bash(", "Bash()",
		"PowerShell", "Write", "Edit", "WebFetch", "Monitor", "mcp__github__*",
		"Bash(git *)", "Bash(git:*)", "Bash(git -c *)", "Bash(gh *)", "Bash(gh:*)",
		"Bash(gh alias *)", "Bash(gh api *)", "Bash(make *)", "Bash(container *)",
		"Bash(gh api -X *)", "Bash(git config *)", "Bash(git rebase *)", "Bash(make test *)",
		"Bash(make lint *)", "Bash(make check-local *)", "Bash(container run *)", "Bash(container exec:*)",
		"Bash(gh alias set *)", "Bash(gh extension install *)", "Bash(git log *)", "Bash(git commit *)",
		"Bash(gh run list *)", "Bash(gh api repos/wstein/workharbor/issues *)", `Bash(git "config" *)`,
		"Bash(git)", "Bash(make)", "Bash(gh run list:* --x)",
	} {
		if allowRuleHasKnownFirstWord(rule) {
			t.Errorf("rule %q must fail the first-word check", rule)
		}
	}
	for _, rule := range []string{
		"Grep", "Glob", "WebSearch", "Bash(git status)", "Bash(make check)", "Bash(gh run list:*)",
		"Bash(df -h)", "Bash(container ls --all)", "Bash(scripts/board-snapshot.sh queue:*)",
		"Bash(scripts/board-snapshot.sh card:*)", "Bash(gh run view:*)", "Bash(gh release list:*)",
		"Bash(scripts/board-snapshot.sh --refresh)",
	} {
		if !allowRuleHasKnownFirstWord(rule) {
			t.Errorf("rule %q must pass the first-word check", rule)
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
	// A trailing ":*" is the legacy spelling of " *"; "*" may appear anywhere.
	if prefix, legacy := strings.CutSuffix(spec, ":*"); legacy {
		return command == prefix || wildcardMatch(prefix+" *", command)
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

func TestClaudeAllowRuleCoversBash(t *testing.T) {
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
		{"Bash(scripts/*:*)", true},
		{"Bash(*/board-snapshot.sh:*)", true},
		{"Bash(*:*)", true},
		{"Bash(scripts/board-snapshot.sh --refresh:*)", false},
		{"Bash(scripts/board-snapshot.sh move 1 Done)", true},
		{"Bash(scripts/board-snapshot.sh move 42 Done)", false},
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

func TestClaudeAllowRuleCoversInterpreterAndFlagFirst(t *testing.T) {
	for _, tc := range []struct {
		rule, command string
		want          bool
	}{
		{"Bash(./scripts/*:*)", "./scripts/board-snapshot.sh move 1 Done", true},
		{"Bash(bash:*)", "bash scripts/board-snapshot.sh move 1 Done", true},
		{"Bash(bash scripts/*)", "bash scripts/board-snapshot.sh move 1 Done", true},
		{"Bash(sh *)", "sh scripts/board-snapshot.sh move 1 Done", true},
		{"Bash(scripts/board-snapshot.sh --refresh:*)", "scripts/board-snapshot.sh --refresh move 1 Done", true},
		{"Bash(scripts/board-snapshot.sh move 42 Done)", "scripts/board-snapshot.sh move 42 Done", true},
		{"Bash(scripts/board-snapshot.sh priority 7 P1)", "scripts/board-snapshot.sh priority 7 P1", true},
		{"Bash(scripts/board-snapshot.sh card:*)", "bash scripts/board-snapshot.sh move 1 Done", false},
	} {
		if got := allowRuleCoversBash(tc.rule, tc.command); got != tc.want {
			t.Errorf("allowRuleCoversBash(%q, %q) = %v, want %v", tc.rule, tc.command, got, tc.want)
		}
	}
}
