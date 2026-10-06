package commitlint

import (
	"strings"
	"testing"
)

// FuzzLint feeds the commit message parser whatever a branch holds. Invariants: it
// never panics, it is deterministic, the number and size of the problems stay
// bounded by the message, and the rule that matters most holds for every message:
// a bot or agent author can never carry a Signed-off-by (it certifies human
// origin), however the message before it is shaped.
func FuzzLint(f *testing.F) {
	for _, s := range []string{
		"docs: add README",
		"feat(domain): add state\n\nRefs: #12",
		"fix: a\n\nRefs: 12",
		"docs: a\n\nAssisted-by: Claude Code:claude-sonnet-5-5 [gopls]",
		"docs: a\n\nCo-Authored-By: Codex gpt-6.1-sol <noreply@openai.com>",
		"docs: a\n\nWhr-Task: t-1\nWhr-Run: r_2",
		"fixup! feat: x",
		"Merge branch 'x'",
		"docs: a\r\n\r\nSigned-off-by: A <a@b>\r\n",
		"",
		"\n\n\n",
		strings.Repeat("a", 5000),
		"docs: " + strings.Repeat("é", 100),
	} {
		f.Add(s, "Werner Stein <claude@wstein.de>", false)
	}
	f.Add("docs: a", "Claude <noreply@anthropic.com>", true)
	f.Fuzz(func(t *testing.T, msg, author string, final bool) {
		opt := Options{Author: author, Final: final}
		got := Lint(msg, opt)
		if again := Lint(msg, opt); strings.Join(got, "\x00") != strings.Join(again, "\x00") {
			t.Fatalf("not deterministic for %q", msg)
		}
		lines := strings.Count(msg, "\n") + 1
		if len(got) > lines*4+16 {
			t.Fatalf("%d problems for %d lines", len(got), lines)
		}
		for _, p := range got {
			if len(p) > 4*(len(msg)+len(author))+1000 { // a quoted value grows up to four times (\x8f)
				t.Fatalf("a problem of %d bytes for a message of %d", len(p), len(msg))
			}
		}
		// the human-origin rule, for any message in front of it: a bot or agent author
		// with a Signed-off-by is never accepted (a message so broken that its
		// trailer is not read as one is rejected for that)
		bot := Options{Author: "Claude <noreply@anthropic.com>", Final: final}
		if got := Lint(msg+"\n\nSigned-off-by: Someone <someone@example.org>", bot); len(got) == 0 {
			t.Fatalf("a bot author's Signed-off-by was accepted after %q", msg)
		}
	})
}
