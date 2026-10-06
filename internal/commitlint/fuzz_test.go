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
	f.Add("docs: a\n# ------------------------ >8", "Claude <noreply@anthropic.com>", false)
	f.Add("docs: a\n# ------------------------ >8\n\nCo-Authored-By: P <p@x.org>", "Claude <noreply@anthropic.com>", false)
	f.Add("docs: a\n\nSigned-off-by: P\n# ------------------------ >8 ------------------------\n\nx", "Claude <noreply@anthropic.com>", false)
	f.Add("docs: a\n\nSigned-off-by:P", "Claude <noreply@anthropic.com>", false)
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
		signed := msg + "\n\nSigned-off-by: Someone <someone@example.org>"
		if got := Lint(signed, bot); len(got) == 0 {
			t.Fatalf("a bot author's Signed-off-by was accepted after %q", msg)
		}
		// a bot or agent author never takes a person as coauthor
		person := msg + "\n\nCo-Authored-By: Someone <someone@example.org>"
		if got := Lint(person, bot); len(got) == 0 {
			t.Fatalf("a bot author's person coauthor was accepted after %q", msg)
		}
		// a trailer in front of git's full cut line counts, the text after it
		// (a stored message keeps it) must not hide it, in either mode
		if !strings.Contains(msg, "# ------------------------ >8") {
			for _, trailer := range []string{"Signed-off-by: Someone <someone@example.org>", "Co-Authored-By: Someone <someone@example.org>"} {
				before := msg + "\n\n" + trailer + "\n# ------------------------ >8 ------------------------\n\nafter"
				for _, o := range []Options{bot, {Author: bot.Author, Final: final, Scissors: true}} {
					if got := Lint(before, o); len(got) == 0 {
						t.Fatalf("%+v: a trailer before the cut line was accepted after %q", o, msg)
					}
				}
			}
		}
		// hook mode cuts at a scissors line at the start of a line, as git commit
		// does; only then may the sign-off be hidden
		bot.Scissors = true
		if !strings.HasPrefix(msg, "# ------------------------ >8") && !strings.Contains(msg, "\n# ------------------------ >8") {
			if got := Lint(signed, bot); len(got) == 0 {
				t.Fatalf("a bot author's Signed-off-by was accepted in hook mode after %q", msg)
			}
			if got := Lint(person, bot); len(got) == 0 {
				t.Fatalf("a bot author's person coauthor was accepted in hook mode after %q", msg)
			}
		}
	})
}
