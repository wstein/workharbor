package scripts_test

import (
	"os/exec"
	"strings"
	"testing"
)

func pathClass(t *testing.T, input string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "path-class.sh") //nolint:gosec // fixed script
	cmd.Dir = "."
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("path-class.sh: %v", err)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func TestPathClass(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, want string
	}{
		{"empty input", "", "ordinary"},
		{"blank lines only", "\n\n", "ordinary"},
		{"exitcode", "internal/exitcode/x.go\n", "ordinary"},
		{"version", "internal/version/v.go\n", "ordinary"},
		{"docscheck", "internal/docscheck/a/b.go\n", "ordinary"},
		{"docs md", "docs/guide.md\n", "ordinary"},
		{"docs md upper case", "DOCS/Guide.MD\n", "ordinary"},
		{"readme", "README.md\n", "ordinary"},
		{"changelog", "CHANGELOG.md\n", "ordinary"},
		{"contributing", "contributing.md\n", "ordinary"},
		{"license", "LICENSE\n", "ordinary"},
		{"no trailing newline", "license", "ordinary"},
		{"several ordinary", "README.md\ninternal/version/v.go\n", "ordinary"},
		{"agents.md", "AGENTS.md\n", "carve-out"},
		{"nested agents.md", "sub/Agents.md\n", "carve-out"},
		{"claude.md", "CLAUDE.md\n", "carve-out"},
		{"claude dir", ".claude/settings.json\n", "carve-out"},
		{"nested claude dir", "x/.CLAUDE/y\n", "carve-out"},
		{"agents dir", ".agents/code.md\n", "carve-out"},
		{"nested agents dir", "x/.agents/y\n", "carve-out"},
		{"github", ".github/workflows/ci.yml\n", "carve-out"},
		{"design docs", "docs/content/docs/design/x.md\n", "carve-out"},
		{"threat docs", "docs/content/docs/Threat-Model.md\n", "carve-out"},
		{"other docs dir md is ordinary only at depth 1", "docs/content/guide.md\n", "ordinary"},
		{"go source", "internal/service/s.go\n", "carve-out"},
		{"scripts", "scripts/land.sh\n", "carve-out"},
		{"unlisted internal sibling", "internal/exitcodex/a.go\n", "carve-out"},
		{"readme in subdir", "sub/README.md\n", "carve-out"},
		{"one carve-out decides", "README.md\nscripts/land.sh\nLICENSE\n", "carve-out"},
		{"carve-out last without newline", "README.md\nAGENTS.md", "carve-out"},
		{"space in name", "docs/my file.md\n", "ordinary"},
		{"space in carve-out name", "scripts/my file.sh\n", "carve-out"},
		{"glob characters are data", "internal/exitcode/*\n[a-z]\n", "carve-out"},
		{"glob only ordinary", "docs/*.md\n", "ordinary"},
		{"quote and dollar", "docs/$(touch x).md\n", "ordinary"},
		{"backslash", "docs/a\\b.md\n", "ordinary"},
		{"leading dash", "-n\n", "carve-out"},
		{"leading blanks are part of the path", " README.md\n", "carve-out"},
		{"non-ASCII name", "docs/ä.md\n", "ordinary"},
		{"newline in a name splits", "docs/a\nb.md\n", "carve-out"},
		{"CR is part of the path", "README.md\r\n", "carve-out"},
		{"empty lines skipped", "\nREADME.md\n\n", "ordinary"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pathClass(t, tc.input); got != tc.want {
				t.Errorf("class(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
