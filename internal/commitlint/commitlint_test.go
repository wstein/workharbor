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
		{"merge with a bot signoff", "Merge branch 'x'\n\nSigned-off-by: Bot <b@x.de>", "Claude <noreply@anthropic.com>", "Signed-off-by"},
		{"merge by a human with a signoff", "Merge branch 'x'\n\nSigned-off-by: W <w@x.de>", human, ""},
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

func TestConventionalChecksOnlyTheSubjectShape(t *testing.T) {
	t.Parallel()
	for msg, ok := range map[string]bool{
		"feat(x): add a thing":                    true,
		"fix: a bug\n\nbody":                      true,
		strings.Repeat("a", 90):                   false,
		"feat(x): " + strings.Repeat("long ", 30): true, // no length rule, no trailer rule
		"feat(x): add a thing\nno blank line":     false,
		"Add a thing":                             false,
		"":                                        false,
		"Merge branch 'x'":                        true,
		"feat(x): s\n\nSigned-off-by: A <a@example.test>\n":     true,
		"feat(x): without the issue trailer feat requires\n\nb": true,
	} {
		if got := len(Conventional(msg)) == 0; got != ok {
			t.Errorf("%q: accepted=%v, want %v (%v)", msg, got, ok, Conventional(msg))
		}
	}
}

func TestConventionalIsFinalAndRefusesAnUnsquashedCommit(t *testing.T) {
	t.Parallel()
	for _, subject := range []string{"fixup! feat: a", "squash! feat: a", "amend! feat: a"} {
		got := Conventional(subject)
		if len(got) != 1 || !strings.Contains(got[0], "autosquash") {
			t.Errorf("%q: %v", subject, got)
		}
	}
	for _, subject := range []string{"Merge branch 'x'", "Revert \"feat: a\""} {
		if got := Conventional(subject); len(got) != 0 {
			t.Errorf("%q: %v", subject, got)
		}
	}
}

func TestCoauthorAttribution(t *testing.T) {
	const human = "Werner Stein <claude@wstein.de>"
	const contributor = "Co-Authored-By: Contributor <contributor@example.test>"
	tests := []struct{ name, trailers, want string }{
		{"human", contributor, ""},
		{"legacy and human", "Assisted-by: codex:gpt-6.1-sol\n" + contributor, ""},
		{"legacy invalid", "assisted-by: codex", "Assisted-by"},
		{"claude display model", "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>", ""},
		{"claude exact model", "Co-Authored-By: Claude claude-sonnet-5-5 <noreply@anthropic.com>", ""},
		{"codex", "Co-Authored-By: Codex gpt-6.1-sol <noreply@openai.com>", ""},
		{"unknown", "Co-Authored-By: Codex unknown <noreply@openai.com>", ""},
		{"case insensitive", "CO-AUTHORED-BY: Codex gpt-6.1-sol <noreply@openai.com>", ""},
		{"multiple tools and human", "Co-Authored-By: Codex gpt-6.1-sol <noreply@openai.com>\nCo-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>\n" + contributor, ""},
		{"missing model", "Co-Authored-By: Claude <noreply@anthropic.com>", "Co-Authored-By"},
		{"codex missing model", "Co-Authored-By: Codex <noreply@openai.com>", "Co-Authored-By"},
		{"wrong address", "Co-Authored-By: Codex gpt-6.1-sol <noreply@anthropic.com>", "Co-Authored-By"},
		{"human named Claude", "Co-Authored-By: Claude Martin <claude@example.test>", ""},
		{"missing vendor", "Co-Authored-By: gpt-6.1-sol <noreply@openai.com>", "Co-Authored-By"},
		{"malformed", "Co-Authored-By: Codex gpt-6.1-sol", "Co-Authored-By"},
		{"invalid model", "Co-Authored-By: Codex ??? <noreply@openai.com>", "Co-Authored-By"},
	}
	for _, subject := range []string{"docs: a", "Merge branch 'x'", `Revert "docs: a"`, "fixup! docs: a", "squash! docs: a", "amend! docs: a"} {
		for _, tc := range tests {
			t.Run(subject+"/"+tc.name, func(t *testing.T) {
				got := Lint(subject+"\n\n"+tc.trailers, Options{Author: human})
				if tc.want == "" && len(got) != 0 || tc.want != "" && !strings.Contains(strings.Join(got, "\n"), tc.want) {
					t.Fatalf("want %q, got %v", tc.want, got)
				}
			})
		}
	}
}

func TestCoauthorsPreserveHumanOnlySignoff(t *testing.T) {
	for _, author := range []string{"Claude <noreply@anthropic.com>", "Codex <noreply@openai.com>", "ci-agent <agent@example.test>"} {
		for _, subject := range []string{"docs: a", "Merge branch 'x'"} {
			msg := subject + "\n\nCo-Authored-By: Codex gpt-6.1-sol <noreply@openai.com>"
			if got := Lint(msg, Options{Author: author}); len(got) != 0 {
				t.Fatalf("agent coauthor: %v", got)
			}
			for _, key := range []string{"Signed-off-by", "SIGNED-OFF-BY"} {
				if got := Lint(msg+"\n"+key+": Someone <someone@example.test>", Options{Author: author}); !strings.Contains(strings.Join(got, "\n"), "Signed-off-by") {
					t.Fatalf("agent signoff accepted: %v", got)
				}
			}
		}
	}
	msg := "docs: a\n\nCo-Authored-By: Codex gpt-6.1-sol <noreply@openai.com>\nSigned-off-by: Human <human@example.test>"
	if got := Lint(msg, Options{Author: "Human <human@example.test>"}); len(got) != 0 {
		t.Fatalf("human signoff: %v", got)
	}
}

func TestAttributionOnlyReadsFinalTrailerBlock(t *testing.T) {
	for _, msg := range []string{
		"docs: a\n\nCo-Authored-By: Claude\nThis is body prose.",
		"docs: a\n\nCo-Authored-By: Claude\n\nRefs: #301",
		"docs: a\n\nAssisted-by: missing model\n\nRefs: #301",
	} {
		if got := Lint(msg, Options{Author: "Human <human@example.test>"}); len(got) != 0 {
			t.Fatalf("body treated as attribution: %v", got)
		}
	}
}
