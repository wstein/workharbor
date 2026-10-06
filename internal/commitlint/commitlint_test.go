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

// Git reads "Key : value", "Key:value" and "Key:\tvalue" as trailers, so the
// linter must see them too (commitlint reads only "Key: value" for Refs and the
// like, but never lets a looser shape hide a sign-off or an AI line).
func TestLenientTrailerShapesAreSeen(t *testing.T) {
	const bot = "Claude <noreply@anthropic.com>"
	const human = "Werner Stein <claude@wstein.de>"
	for _, sig := range []string{
		"Signed-off-by : P <p@example.test>",
		"Signed-off-by\t: P <p@example.test>",
		"Signed-off-by:P <p@example.test>",
		"Signed-off-by:\tP <p@example.test>",
		"signed-off-by :P <p@example.test>",
	} {
		for _, msg := range []string{
			"docs: a\n\n" + sig,
			"docs: a\n\nRefs: #1\n" + sig,
			"Merge branch 'x'\n\n" + sig,
		} {
			if got := Lint(msg, Options{Author: bot}); !strings.Contains(strings.Join(got, "\n"), "Signed-off-by") {
				t.Errorf("bot signoff %q accepted: %v", msg, got)
			}
			if got := Lint(msg, Options{Author: human}); len(got) != 0 {
				t.Errorf("human signoff %q: %v", msg, got)
			}
		}
	}
	// git's 25% rule: a recognised Signed-off-by with prose lines is a trailer block
	prose := "docs: a\n\nSigned-off-by: P <p@example.test>\nsome prose\nmore prose"
	if got := Lint(prose, Options{Author: bot}); !strings.Contains(strings.Join(got, "\n"), "Signed-off-by") {
		t.Errorf("bot signoff with prose accepted: %v", got)
	}
	// ... and the same shapes cannot hide an invalid AI line
	for _, line := range []string{
		"Co-authored-by : Claude <noreply@anthropic.com>",
		"Co-Authored-By : Claude <noreply@anthropic.com>",
		"Co-Authored-By:Claude <noreply@anthropic.com>",
		"Co-Authored-By:\tCodex <noreply@openai.com>",
		"Assisted-by : x",
		"Assisted-by:x",
	} {
		for _, msg := range []string{"docs: a\n\n" + line, "docs: a\n\nRefs: #1\n" + line, "Merge branch 'x'\n\n" + line} {
			if got := Lint(msg, Options{Author: human}); len(got) == 0 {
				t.Errorf("invalid attribution %q accepted", msg)
			}
		}
	}
	// valid spaced AI lines stay valid
	for _, line := range []string{
		"Co-Authored-By : Claude Sonnet 5.5 <noreply@anthropic.com>",
		"Co-Authored-By:Claude Sonnet 5.5 <noreply@anthropic.com>",
		"Co-authored-by : Person <person@example.test>",
	} {
		if got := Lint("docs: a\n\n"+line, Options{Author: human}); len(got) != 0 {
			t.Errorf("%q: %v", line, got)
		}
	}
	// prose is not a trailer block without a recognised Signed-off-by
	if got := Lint("docs: a\n\nAssisted-by : x\nprose line", Options{Author: human}); len(got) != 0 {
		t.Errorf("prose paragraph read as trailers: %v", got)
	}
}

// Scissors cut a message only in hook mode (git commit cleanup); a stored
// message keeps everything after such a line.
func TestScissorsOnlyCutInHookMode(t *testing.T) {
	const human = "Werner Stein <claude@wstein.de>"
	const bot = "Claude <noreply@anthropic.com>"
	const cut = "# ------------------------ >8 ------------------------"
	hidden := "fix: a\n\nRefs: #1\n" + cut + "\n\nSigned-off-by: P <p@example.test>"
	if got := Lint(hidden, Options{Author: bot}); !strings.Contains(strings.Join(got, "\n"), "Signed-off-by") {
		t.Errorf("stored scissors hid a signoff: %v", got)
	}
	if got := Lint(hidden, Options{Author: bot, Scissors: true}); len(got) != 0 {
		t.Errorf("hook mode must cut at scissors: %v", got)
	}
	bad := "docs: a\n" + cut + "\n\nCo-Authored-By: Claude <noreply@anthropic.com>"
	if got := Lint(bad, Options{Author: human}); len(got) == 0 {
		t.Error("stored scissors hid an invalid AI line")
	}
	if got := Lint(bad, Options{Author: human, Scissors: true}); len(got) != 0 {
		t.Errorf("hook mode: %v", got)
	}
	if got := Conventional("docs: a\n" + cut + "\ndiff --git"); len(got) == 0 {
		t.Error("conventional is a stored-message check and must not cut at scissors")
	}
	if got := Lint("docs: a\n"+cut+"\ndiff --git", Options{Author: human, Scissors: true}); len(got) != 0 {
		t.Errorf("hook mode cut: %v", got)
	}
}

// Gaps the mutation review found.
func TestAttributionAndSignoffCases(t *testing.T) {
	const human = "Werner Stein <claude@wstein.de>"
	const person = "Co-Authored-By: Person <person@example.test>"
	const ai = "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
	for _, author := range []string{"Claude <noreply@anthropic.com>", "ci-agent <agent@example.test>", "dependabot[bot] <support@github.com>"} {
		if got := Lint("docs: a\n\n"+ai+"\n"+person, Options{Author: author}); len(got) != 0 {
			t.Errorf("%s with human coauthor: %v", author, got)
		}
	}
	if got := Lint("build(deps): bump x\n\n"+person+"\nSigned-off-by: dependabot[bot] <support@github.com>", Options{Author: "dependabot[bot] <support@github.com>"}); len(got) != 0 {
		t.Errorf("dependabot with person coauthor: %v", got)
	}
	for _, p := range []string{"Merge ", "Revert ", "fixup! ", "squash! ", "amend! "} {
		msg := p + "x\n\n" + person
		if got := Lint(msg, Options{Author: "ci-agent <agent@example.test>"}); len(got) != 0 {
			t.Errorf("%q bot with person coauthor: %v", p, got)
		}
		if got := Lint(msg+"\nSigned-off-by: P <p@example.test>", Options{Author: "ci-agent <agent@example.test>"}); len(got) == 0 {
			t.Errorf("%q bot signoff accepted", p)
		}
	}
	for _, key := range []string{"Co-Authored-By", "co-authored-by", "CO-AUTHORED-BY", "Co-authored-by"} {
		if got := Lint("docs: a\n\n"+key+": Claude <noreply@anthropic.com>", Options{Author: human}); len(got) == 0 {
			t.Errorf("%s: invalid AI line accepted", key)
		}
		if got := Lint("docs: a\n\n"+key+": Claude Sonnet 5.5 <noreply@anthropic.com>", Options{Author: human}); len(got) != 0 {
			t.Errorf("%s: %v", key, got)
		}
	}
	for _, email := range []string{"NOREPLY@ANTHROPIC.COM", "NoReply@Anthropic.com"} {
		if got := Lint("docs: a\n\nCo-Authored-By: Claude <"+email+">", Options{Author: human}); len(got) == 0 {
			t.Errorf("%s: invalid AI line accepted", email)
		}
		if got := Lint("docs: a\n\nCo-Authored-By: Codex Sonnet <"+email+">", Options{Author: human}); len(got) == 0 {
			t.Errorf("%s: wrong vendor accepted", email)
		}
		if got := Lint("docs: a\n\nCo-Authored-By: Claude Sonnet 5.5 <"+email+">", Options{Author: human}); len(got) != 0 {
			t.Errorf("%s: %v", email, got)
		}
	}
}
