package commitlint

import (
	"fmt"
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
		{"antigravity", "Co-Authored-By: Antigravity Gemini 3.8 Flash <noreply@google.com>", ""},
		{"antigravity upper address", "Co-Authored-By: Antigravity Gemini 3.8 Flash <NoReply@Google.com>", ""},
		{"antigravity missing model", "Co-Authored-By: Antigravity <noreply@google.com>", "Co-Authored-By"},
		{"antigravity wrong vendor", "Co-Authored-By: Alice <noreply@google.com>", "Co-Authored-By"},
		{"antigravity on the wrong address", "Co-Authored-By: Antigravity Gemini <noreply@openai.com>", "Co-Authored-By"},
		{"human at google", "Co-Authored-By: Alice <alice@google.com>", ""},
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
	// a human author keeps a human coauthor next to an AI line (H7)
	if got := Lint("docs: a\n\n"+ai+"\n"+person, Options{Author: human}); len(got) != 0 {
		t.Errorf("human author with AI and person coauthors: %v", got)
	}
	if got := Lint("build(deps): bump x\n\n"+person+"\nSigned-off-by: dependabot[bot] <support@github.com>", Options{Author: "dependabot[bot] <support@github.com>"}); len(got) != 0 {
		t.Errorf("dependabot with person coauthor: %v", got)
	}
	for _, p := range []string{"Merge ", "Revert ", "fixup! ", "squash! ", "amend! "} {
		msg := p + "x\n\n" + ai
		if got := Lint(msg, Options{Author: "ci-agent <agent@example.test>"}); len(got) != 0 {
			t.Errorf("%q bot with AI coauthor: %v", p, got)
		}
		if got := Lint(p+"x\n\n"+person, Options{Author: "ci-agent <agent@example.test>"}); len(got) == 0 {
			t.Errorf("%q bot with person coauthor accepted", p)
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

func TestAntigravityIdentityAndMessages(t *testing.T) {
	const ag = "Co-Authored-By: Antigravity Gemini 3.8 Flash <noreply@google.com>"
	for _, author := range []string{"Antigravity <noreply@google.com>", "Antigravity Gemini <noreply@google.com>"} {
		if got := Lint("docs: a\n\n"+ag, Options{Author: author}); len(got) != 0 {
			t.Errorf("%s: %v", author, got)
		}
		if got := Lint("docs: a\n\n"+ag+"\nSigned-off-by: P <p@example.test>", Options{Author: author}); !strings.Contains(strings.Join(got, "\n"), "Signed-off-by") {
			t.Errorf("%s signoff accepted: %v", author, got)
		}
	}
	for addr, want := range map[string]string{
		"noreply@anthropic.com": "Claude <model-id> <noreply@anthropic.com>",
		"noreply@openai.com":    "Codex <model-id> <noreply@openai.com>",
		"noreply@google.com":    "Antigravity <model-id> <noreply@google.com>",
	} {
		got := Lint("docs: a\n\nCo-Authored-By: Alice <"+addr+">", Options{Author: "W <w@x.de>"})
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Errorf("%s: want %q in %v", addr, want, got)
		}
	}
}

// A bot or agent author takes no person as coauthor: only AI attribution on a
// reserved address. A human author keeps human coauthors (AGENTS.md).
func TestBotAuthorsTakeNoPersonCoauthors(t *testing.T) {
	const person = "Co-Authored-By: Person <person@example.test>"
	const ai = "Co-Authored-By: Codex gpt-6.1-sol <noreply@openai.com>"
	bots := []string{
		"Claude <noreply@anthropic.com>", "Codex <noreply@openai.com>", "Antigravity <noreply@google.com>",
		"ci-agent <agent@example.test>", "release-bot <bot@example.test>", "github-actions[bot] <a@users.noreply.github.com>",
	}
	for _, author := range bots {
		for _, subject := range []string{"docs: a", "feat: a\n\nRefs: #1", "Merge branch 'x'", "fixup! docs: a"} {
			for _, line := range []string{person, "co-authored-by : Person <person@example.test>", "Co-Authored-By:Person <person@example.test>", "Co-Authored-By: Claude Martin <claude@example.test>", "Co-Authored-By: Alice <alice@google.com>"} {
				sep := "\n\n"
				if strings.Contains(subject, "Refs") {
					sep = "\n"
				}
				got := Lint(subject+sep+ai+"\n"+line, Options{Author: author})
				if !strings.Contains(strings.Join(got, "\n"), "person") {
					t.Errorf("%s: %q accepted: %v", author, line, got)
				}
				if got := Lint(subject+sep+line, Options{Author: author, Scissors: true}); len(got) == 0 {
					t.Errorf("%s: %q accepted in hook mode", author, line)
				}
			}
			if got := Lint(subject+"\n\n"+ai, Options{Author: author}); len(got) != 0 && !strings.Contains(subject, "feat") {
				t.Errorf("%s: AI coauthor alone: %v", author, got)
			}
		}
	}
	for _, author := range []string{"Werner Stein <claude@wstein.de>", "dependabot[bot] <support@github.com>", "renovate[bot] <29139614+renovate[bot]@users.noreply.github.com>"} {
		if got := Lint("docs: a\n\n"+ai+"\n"+person, Options{Author: author}); len(got) != 0 {
			t.Errorf("%s: %v", author, got)
		}
	}
}

const fullCut = "# ------------------------ >8 ------------------------"

// Git cuts a stored message at its full cut line when it reads trailers, so a
// trailer before the line counts even though the text after it is kept.
func TestTrailerBeforeAStoredScissorsLineIsStillSeen(t *testing.T) {
	const bot = "Claude <noreply@anthropic.com>"
	const human = "Werner Stein <claude@wstein.de>"
	for _, line := range []string{
		"Signed-off-by: P <p@example.test>",
		"Co-Authored-By: Person <person@example.test>",
		"Co-Authored-By: Claude <noreply@anthropic.com>",
	} {
		for _, subject := range []string{"docs: a", "Merge branch 'x'", "fixup! docs: a"} {
			msg := subject + "\n\n" + line + "\n" + fullCut + "\n\nprose after"
			for _, opt := range []Options{{Author: bot}, {Author: bot, Final: true}, {Author: bot, Scissors: true}} {
				if got := Lint(msg, opt); len(got) == 0 {
					t.Errorf("%+v: %q accepted", opt, msg)
				}
			}
		}
	}
	// a human author keeps Signed-off-by and a person before the line
	msg := "docs: a\n\nSigned-off-by: P <p@example.test>\nCo-Authored-By: Person <person@example.test>\n" + fullCut + "\n\nprose after"
	if got := Lint(msg, Options{Author: human}); len(got) != 0 {
		t.Errorf("human: %v", got)
	}
	// and a trailer after the line is still seen in a stored message (F2), not in hook mode
	after := "docs: a\n\nRefs: #1\n" + fullCut + "\n\nSigned-off-by: P <p@example.test>"
	if got := Lint(after, Options{Author: bot, Final: true}); len(got) == 0 {
		t.Error("stored scissors hid a trailer after the line")
	}
	if got := Lint(after, Options{Author: bot, Scissors: true}); len(got) != 0 {
		t.Errorf("hook mode: %v", got)
	}
}

// Git's own trailer-block rules decide when an invalid AI line is read.
func TestTrailerBlockFollowsGit(t *testing.T) {
	const human = "Werner Stein <claude@wstein.de>"
	prose := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "prose %d\n", i)
		}
		return b.String()
	}
	const bad = "Assisted-by : x" // invalid whenever it is read as a trailer
	cont := "  c1\n  c2\n  c3\n  c4\n  c5\n  c6\n  c7\n"
	tests := []struct {
		name, para string
		read       bool
	}{
		{"signoff and six prose lines", "Signed-off-by: Q <q@example.test>\n" + prose(6) + bad, true},
		{"signoff and seven prose lines", "Signed-off-by: Q <q@example.test>\n" + prose(7) + bad, false},
		{"signoff and one prose line", "Signed-off-by: Q <q@example.test>\n" + prose(1) + bad, true},
		{"cherry pick line and six prose lines", "(cherry picked from commit abc)\n" + prose(6) + bad, true},
		{"cherry pick line and seven prose lines", "(cherry picked from commit abc)\n" + prose(7) + bad, false},
		{"lowercase signoff is not recognised", "signed-off-by: Q <q@example.test>\n" + prose(6) + bad, false},
		{"spaced signoff is not recognised", "Signed-off-by : Q <q@example.test>\n" + prose(6) + bad, false},
		{"continuation lines belong to the trailer above", "Signed-off-by: Q <q@example.test>\n" + cont + bad, true},
		{"continuation lines under prose count against", "prose\n" + cont + "Refs: #1\n" + bad, false},
		{"prose above a signoff counts against the block", prose(8) + "Signed-off-by: Q <q@example.test>\n" + bad, false},
		{"bad line above a final signoff", bad + "\nSigned-off-by: Q <q@example.test>", true},
		{"bad line above a final cherry-pick line", bad + "\n(cherry picked from commit abc)", true},
		{"bad line above signoff and prose", bad + "\nSigned-off-by: Q <q@example.test>\nprose", true},
		{"prose after a continuation weighs one plus its lines", "Signed-off-by: Q <q@example.test>\nprose\n" + cont + bad, false},
		{"a trailer ends the continuation count", "prose\nSigned-off-by: Q <q@example.test>\n" + cont + bad, true},
		{"continuation with no line above", "\tc1\nRefs: #1\n" + bad, false},
		{"carriage return line is blank", "prose\n\r\r\n" + bad, true},
		{"carriage return continuation lines", "Signed-off-by: Q <q@example.test>\n" + strings.Repeat("\rc\n", 7) + bad, true},
		{"digit token", "1x: y\n" + bad, true},
		{"dash token", "-x: y\n" + bad, true},
		{"no recognised prefix and one prose line", "prose\n" + bad, false},
	}
	for _, tc := range tests {
		got := Lint("docs: a\n\n"+tc.para, Options{Author: human})
		if tc.read != (len(got) != 0) {
			t.Errorf("%s: read=%v, got %v", tc.name, tc.read, got)
		}
	}
}

// A bot or agent author takes no person coauthor in any shape git reads.
func TestBotAuthorPersonCoauthorShapesGitReads(t *testing.T) {
	const person = "Co-Authored-By: Person <person@example.test>"
	for _, author := range []string{"ci-agent <agent@example.test>", "Claude <noreply@anthropic.com>"} {
		for name, para := range map[string]string{
			"cherry pick and three prose lines": "(cherry picked from commit abc)\nl1\nl2\nl3\n" + person,
			"digit token":                       "1x: y\n" + person,
			"dash token":                        "-x: y\n" + person,
			"leading carriage return":           "l1\nl2\nl3\n\r" + person,
			"carriage return line":              "l1\n\r\r\n" + person,
			"carriage return after prose":       "l1\r\n\r\r\nl2\r\n" + person,
			"prose around it":                   "l1\n" + person + "\nl2",
			"spaced key":                        "co-authored-by : Person <person@example.test>",
			"no space":                          "Co-Authored-By:Person <person@example.test>",
			"indented":                          "Refs: #1\n  " + person,
		} {
			for _, subject := range []string{"docs: a", "Merge branch 'x'"} {
				for _, opt := range []Options{{Author: author}, {Author: author, Scissors: true}} {
					if got := Lint(subject+"\n\n"+para, opt); len(got) == 0 {
						t.Errorf("%s/%s/%+v: accepted", author, name, opt)
					}
				}
			}
		}
	}
	// a human author may keep the person
	if got := Lint("docs: a\n\nl1\nl2\n\r"+person, Options{Author: "Werner Stein <claude@wstein.de>"}); len(got) != 0 {
		t.Errorf("human: %v", got)
	}
}

// A signed-off line git does not read as a trailer still counts against a bot.
func TestBotSignoffOutsideAGitTrailerBlock(t *testing.T) {
	got := Lint("docs: a\n\nSigned-off-by : P <p@example.test>\nprose", Options{Author: "Claude <noreply@anthropic.com>"})
	if !strings.Contains(strings.Join(got, "\n"), "Signed-off-by") {
		t.Errorf("accepted: %v", got)
	}
}

// Only git's full cut line ends the trailers git reads in a stored message.
func TestAPartialCutLineDoesNotHideTrailersFromGitsReading(t *testing.T) {
	msg := "docs: a\n\nSigned-off-by: P <p@example.test>\n# ------------------------ >8\n\nprose"
	if got := Lint(msg, Options{Author: "Claude <noreply@anthropic.com>"}); len(got) != 0 {
		t.Errorf("git reads the last paragraph, which holds no trailer: %v", got)
	}
}

// Git cuts at its exact full cut line only; a loose one (trailing space, tab,
// carriage return) is text, so a trailer after it and before an exact line is
// read by git.
func TestALooseCutLineDoesNotHideTrailersBeforeAnExactOne(t *testing.T) {
	const bot = "Claude <noreply@anthropic.com>"
	for _, loose := range []string{fullCut + " ", fullCut + "\t", fullCut + "\r", fullCut + " \r", "# ------------------------ >8"} {
		for _, line := range []string{"Signed-off-by: P <p@example.test>", "Co-Authored-By: Person <person@example.test>"} {
			for _, subject := range []string{"docs: a", "Merge branch 'x'"} {
				msg := subject + "\n\n" + loose + "\n" + line + "\n" + fullCut + "\n\nprose"
				for _, opt := range []Options{{Author: bot}, {Author: bot, Final: true}} {
					if got := Lint(msg, opt); len(got) == 0 {
						t.Errorf("%+v: %q accepted", opt, msg)
					}
				}
			}
		}
	}
	// every line ended with CRLF: no line is git's exact cut line, so nothing is cut
	crlf := strings.ReplaceAll("docs: a\n\nSigned-off-by: P <p@example.test>\n"+fullCut+"\n\nprose", "\n", "\r\n")
	if got := Lint(crlf, Options{Author: bot}); len(got) != 0 {
		t.Errorf("git reads the last paragraph of a CRLF message: %v", got)
	}
	// a problem found in both readings is reported once
	got := Lint("docs: a\n\nCo-Authored-By: Person <person@example.test>\n"+fullCut+"\n\nCo-Authored-By: Person <person@example.test>", Options{Author: bot})
	if n := strings.Count(strings.Join(got, "\n"), "is a person"); n != 1 {
		t.Errorf("%d reports: %v", n, got)
	}
}

// A line that makes the paragraph a trailer block makes every trailer of it
// count, the ones above it included.
func TestEveryTrailerOfABlockIsValidated(t *testing.T) {
	const human = "Werner Stein <claude@wstein.de>"
	for _, para := range []string{
		"Co-Authored-By: Claude <noreply@anthropic.com>\nSigned-off-by: H <h@example.test>",
		"Co-Authored-By: Codex gpt-6 <noreply@anthropic.com>\nSigned-off-by: H <h@example.test>",
		"Assisted-by: nonsense\nSigned-off-by: H <h@example.test>",
		"Assisted-by: nonsense\n(cherry picked from commit abc)",
		"Refs: #1\nCo-Authored-By: Claude <noreply@anthropic.com>\nSigned-off-by: H <h@example.test>",
	} {
		for _, opt := range []Options{{Author: human}, {Author: human, Final: true}, {Author: human, Scissors: true}} {
			if got := Lint("docs: a\n\n"+para, opt); len(got) == 0 {
				t.Errorf("%+v: %q accepted", opt, para)
			}
		}
	}
}
