package commitlint

import (
	"strings"
	"testing"
)

func TestLint(t *testing.T) {
	const human = "Werner Stein <claude@wstein.de>"
	tests := []struct {
		name   string
		msg    string
		author string
		want   string // substring of the expected problem; empty means valid
	}{
		{"docs without issue", "docs: add README", human, ""},
		{"chore with scope", "chore(ci): pin action", human, ""},
		{"feat with issue", "feat(domain): add state\n\nRefs: #12", human, ""},
		{"fix closes issue", "fix: handle nil\n\nCloses: #3", human, ""},
		{"cross-repo issue", "perf: speed up\n\nRefs: owner/repo#9, #4", human, ""},
		{"feat without issue", "feat(domain): add state", human, "must reference an issue"},
		{"refactor without issue", "refactor: split file\n\nwhy", human, "must reference an issue"},
		{"bad subject", "added stuff", human, "Conventional Commits"},
		{"subject too long", "docs: " + strings.Repeat("x", 80), human, "characters"},
		{"no blank after subject", "docs: a\nbody", human, "blank line"},
		{"bad issue value", "fix: a\n\nRefs: 12", human, "not an issue reference"},
		{"assisted ok", "docs: a\n\nAssisted-by: Claude Code:claude-sonnet-5-5", human, ""},
		{"assisted with tools", "docs: a\n\nAssisted-by: Claude Code:claude-sonnet-5-5 [gopls]", human, ""},
		{"assisted free-form model", "docs: a\n\nAssisted-by: Other:some-model.v2", human, ""},
		{"assisted no model", "docs: a\n\nAssisted-by: Claude", human, "Assisted-by"},
		{"task and run", "docs: a\n\nWhr-Task: t-1\nWhr-Run: r_2", human, ""},
		{"run without task", "docs: a\n\nWhr-Run: r1", human, "requires a Whr-Task"},
		{"two tasks", "docs: a\n\nWhr-Task: t1\nWhr-Task: t2", human, "at most one"},
		{"bad task id", "docs: a\n\nWhr-Task: bad id", human, "not a valid id"},
		{"human signoff", "docs: a\n\nSigned-off-by: Werner <w@x.de>", human, ""},
		{"bot signoff", "docs: a\n\nSigned-off-by: Bot <b@x.de>", "Claude <noreply@anthropic.com>", "Signed-off-by"},
		{"bot name signoff", "docs: a\n\nSigned-off-by: X <x@x.de>", "ci-bot <ci@x.de>", "Signed-off-by"},
		{"dependabot long subject", "build(deps): " + strings.Repeat("x", 80) + " in /docs", "dependabot[bot] <support@github.com>", ""},
		{"dependabot signoff", "build(deps): bump x\n\nSigned-off-by: dependabot[bot] <support@github.com>", "dependabot[bot] <support@github.com>", ""},
		{"renovate signoff", "build(deps): update y\n\nSigned-off-by: Renovate Bot <bot@renovateapp.com>", "renovate[bot] <29139614+renovate[bot]@users.noreply.github.com>", ""},
		{"human long subject still fails", "build(deps): " + strings.Repeat("x", 80), human, "characters"},
		{"merge exempt", "Merge branch 'x'", human, ""},
		{"fixup exempt", "fixup! feat: a", human, ""},
		{"comments ignored", "docs: a\n\n# Refs: nothing\n# comment", human, ""},
		{"scissors cut", "docs: a\n# ------------------------ >8 ------------------------\ndiff --git", human, ""},
		{"body not trailers", "docs: a\n\nsome body\nRefs: #1 in prose", human, ""},
		{"breaking footer", "feat!: drop api\n\nBREAKING CHANGE: gone\nRefs: #7", human, ""},
		{"changelog skip", "feat: tiny\n\nRefs: #1\nChangelog: skip", human, ""},
		{"changelog highlight", "docs: install guide\n\nChangelog: highlight", human, ""},
		{"changelog bad value", "docs: a\n\nChangelog: hide", human, "Changelog: \"hide\""},
		{"changelog wrong case", "docs: a\n\nChangelog: Skip", human, "Changelog: \"Skip\""},
		{"two changelog trailers", "docs: a\n\nChangelog: skip\nChangelog: highlight", human, "at most one Changelog"},
		{"empty", "\n# only comment", human, "empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Lint(tc.msg, Options{Author: tc.author})
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected valid, got %v", got)
				}
				return
			}
			if !strings.Contains(strings.Join(got, "\n"), tc.want) {
				t.Fatalf("expected problem containing %q, got %v", tc.want, got)
			}
		})
	}
}

func TestAFixupMustBeSquashedBeforeItLands(t *testing.T) {
	for _, subject := range []string{"fixup! feat: a", "squash! feat: a", "amend! feat: a"} {
		if got := Lint(subject, Options{Author: "Werner Stein <w@x.de>"}); len(got) != 0 {
			t.Errorf("%q is a normal commit while working: %v", subject, got)
		}
		got := Lint(subject, Options{Author: "Werner Stein <w@x.de>", Final: true})
		if len(got) != 1 || !strings.Contains(got[0], "autosquash") {
			t.Errorf("%q landing: %v", subject, got)
		}
	}
	for _, subject := range []string{"Merge branch 'x'", "Revert \"feat: a\""} {
		if got := Lint(subject, Options{Author: "Werner Stein <w@x.de>", Final: true}); len(got) != 0 {
			t.Errorf("%q: %v", subject, got)
		}
	}
}
