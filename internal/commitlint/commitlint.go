// Package commitlint validates commit messages: a Conventional Commits
// subject plus Git trailers for issues, AI assistance and whr provenance.
// See AGENTS.md for the rules.
package commitlint

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxSubject = 72

var (
	subjectRe = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?(!)?: \S.*$`)
	trailerRe = regexp.MustCompile(`^(BREAKING[ -]CHANGE|[A-Za-z][A-Za-z0-9-]*): (.*)$`)

	issueRef   = `(?:#[0-9]+|[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+#[0-9]+)`
	issueValue = regexp.MustCompile(`^` + issueRef + `(?:, ` + issueRef + `)*$`)

	// <tool>:<model-id> with optional [extra tools], e.g.
	// "Claude Code:claude-sonnet-5-5 [gopls]".
	assistedRe  = regexp.MustCompile(`^[^:\s][^:]*:[A-Za-z0-9][A-Za-z0-9._/+-]*(?: \[[^\]]+\])*$`)
	coauthorRe  = regexp.MustCompile(`^([^<>\s][^<>]*?) <([^<>\s@]+@[^<>\s@]+)>$`)
	modelNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+ -]*$`)
	idRe        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	botRe       = regexp.MustCompile(`(?i)\[bot\]|\b(?:bot|agent)\b|noreply@(?:anthropic|openai|google)\.com`)

	// scanKeyRe is git's own reading of a trailer line: a token, optional
	// whitespace, a colon, optional whitespace and the value. It is looser than
	// trailerRe, so a spaced or tabbed key cannot hide a line from the rules.
	scanKeyRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*)[ \t]*:[ \t]*(.*)$`)

	// depBotRe matches the dependency-update bots, whose generated messages
	// cannot follow every rule: long titles, and a DCO Signed-off-by line.
	depBotRe = regexp.MustCompile(`(?i)^\s*(?:dependabot|renovate)(?:\[bot\])?\b`)
)

// aiIdentities are the project's reserved attribution addresses and the first
// word of the name that goes with each.
var aiIdentities = map[string]struct{ vendor string }{
	"noreply@anthropic.com": {"Claude"},
	"noreply@openai.com":    {"Codex"},
	"noreply@google.com":    {"Antigravity"},
}

// issueKeys are the trailer tokens that reference an issue.
var issueKeys = map[string]bool{
	"Closes": true, "Fixes": true, "Resolves": true, "Refs": true, "Related": true,
}

// issueRequired lists the commit types that must reference an issue.
var issueRequired = map[string]bool{
	"feat": true, "fix": true, "perf": true, "refactor": true,
}

var exemptPrefixes = []string{"Merge ", "Revert ", "fixup! ", "squash! ", "amend! "}

// Options carries context that is not part of the message itself.
type Options struct {
	// Author is the commit author identity ("Name <email>"). Bot authors may
	// not add Signed-off-by, which certifies human origin. The dependency bots
	// (Dependabot, Renovate) are exempt from the subject length and Signed-off-by
	// rules, because they generate their own messages.
	Author string
	// Final says the commit is about to land, as when a range is checked: a
	// fixup!, squash! or amend! commit must have been squashed away by then
	// (git rebase -i --autosquash). The commit-msg hook leaves it false, since
	// git commit --fixup is how such a commit is made.
	Final bool
	// Scissors cuts the message at a "# ------------------------ >8" line, as
	// git commit does while it cleans up the message the hook sees. A stored
	// message (a range, the host's publish path) keeps such a line as text, and
	// what follows stays visible to the rules.
	Scissors bool
}

type trailer struct{ key, value string }

// Lint returns one problem per violated rule; an empty result means the
// message is acceptable.
func Lint(msg string, opt Options) []string {
	lines := clean(msg, opt.Scissors)
	if len(lines) == 0 {
		return []string{"empty commit message"}
	}
	subject := lines[0]
	scan, block := scanFinalParagraph(lines)
	var attributed []trailer
	if block {
		attributed = scan
	}
	attributionProblems := attributionProblemsFor(attributed, opt)
	for _, p := range exemptPrefixes {
		if strings.HasPrefix(subject, p) {
			// These skip the message rules, never the one about origin: a bot's
			// "Merge ..." must not carry a Signed-off-by either.
			problems := attributionProblems
			if signoffByBot(scan, opt) {
				problems = append(problems, fmt.Sprintf("Signed-off-by certifies human origin; remove it from commits authored by %q", opt.Author))
			}
			if opt.Final && p != "Merge " && p != "Revert " {
				problems = append(problems, fmt.Sprintf("%q commit is not squashed: run git rebase -i --autosquash before it lands", strings.TrimSpace(p)))
			}
			return problems
		}
	}

	problems := attributionProblems
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	m := subjectRe.FindStringSubmatch(subject)
	if m == nil {
		add("subject must follow Conventional Commits, e.g. 'feat(domain): add run state'")
	}
	depBot := depBotRe.MatchString(opt.Author)
	if n := utf8.RuneCountInString(subject); n > maxSubject && !depBot {
		add("subject is %d characters; keep it at %d or fewer", n, maxSubject)
	}
	if len(lines) > 1 && lines[1] != "" {
		add("leave a blank line after the subject")
	}

	trailers := parseTrailers(lines)
	var issues, tasks, runs, changelogs int
	for _, t := range trailers {
		switch {
		case issueKeys[t.key]:
			issues++
			if !issueValue.MatchString(t.value) {
				add("%s: %q is not an issue reference; use #123 or owner/repo#123", t.key, t.value)
			}
		case t.key == "Whr-Task":
			tasks++
			if !idRe.MatchString(t.value) {
				add("Whr-Task: %q is not a valid id", t.value)
			}
		case t.key == "Whr-Run":
			runs++
			if !idRe.MatchString(t.value) {
				add("Whr-Run: %q is not a valid id", t.value)
			}
		case t.key == "Changelog":
			changelogs++
			if t.value != "skip" && t.value != "highlight" {
				add("Changelog: %q must be skip or highlight", t.value)
			}
		}
	}

	if m != nil && issueRequired[m[1]] && issues == 0 {
		add("%s commits must reference an issue, e.g. 'Refs: #123' or 'Closes: #123'", m[1])
	}
	if tasks > 1 {
		add("at most one Whr-Task trailer is allowed")
	}
	if changelogs > 1 {
		add("at most one Changelog trailer is allowed")
	}
	if runs > 0 && tasks == 0 {
		add("Whr-Run requires a Whr-Task trailer")
	}
	if signoffByBot(scan, opt) {
		add("Signed-off-by certifies human origin; remove it from commits authored by %q", opt.Author)
	}
	return problems
}

// attributionProblemsFor validates both legacy assistance and current coauthor
// trailers, including on subject-exempt commits. Human coauthors are independent
// of AI attribution on a human's commit; a bot or agent author takes none.
// Reserved project addresses identify AI attribution; names alone may belong
// to humans. The message cannot prove which model actually ran.
func attributionProblemsFor(trailers []trailer, opt Options) []string {
	botAuthor := botRe.MatchString(opt.Author) && !depBotRe.MatchString(opt.Author)
	var problems []string
	for _, t := range trailers {
		if strings.EqualFold(t.key, "Assisted-by") && !assistedRe.MatchString(t.value) {
			problems = append(problems, fmt.Sprintf("Assisted-by: %q must be <tool>:<model-id>", t.value))
		}
		if !strings.EqualFold(t.key, "Co-Authored-By") {
			continue
		}
		m := coauthorRe.FindStringSubmatch(t.value)
		if m == nil {
			problems = append(problems, "Co-Authored-By must be Name <email>; AI attribution requires <tool> <model-id> <attribution-email>")
			continue
		}
		name, email := m[1], strings.ToLower(m[2])
		fields := strings.Fields(name)
		id, ok := aiIdentities[email]
		if !ok {
			if botAuthor {
				problems = append(problems, fmt.Sprintf("Co-Authored-By %q is a person; a commit authored by %q (a bot or agent) takes only AI attribution coauthors", t.value, opt.Author))
			}
			continue // Other identities may be legitimate human coauthors of a human's commit.
		}
		if !strings.EqualFold(fields[0], id.vendor) || !modelNameRe.MatchString(strings.Join(fields[1:], " ")) {
			problems = append(problems, fmt.Sprintf("Co-Authored-By AI attribution requires %s <model-id> <%s>; use the exact exposed model or unknown", id.vendor, email))
		}
	}
	return problems
}

// signoffByBot reports a Signed-off-by line, in any shape git reads as a
// trailer, on a commit whose author is a bot or an agent (the dependency bots
// are exempt).
func signoffByBot(scan []trailer, opt Options) bool {
	if !botRe.MatchString(opt.Author) || depBotRe.MatchString(opt.Author) {
		return false
	}
	for _, t := range scan {
		if strings.EqualFold(t.key, "Signed-off-by") {
			return true
		}
	}
	return false
}

// clean drops git's comment lines and, when scissors is set (the commit-msg
// hook, where git has cleaned the message up the same way), the scissors
// section, then trims trailing blank lines.
func clean(msg string, scissors bool) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n") {
		if scissors && strings.HasPrefix(l, "# ------------------------ >8") {
			break
		}
		if strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// parseTrailers returns the trailers in the final paragraph, which counts as
// a trailer block only if every line is a trailer or a continuation.
func parseTrailers(lines []string) []trailer {
	end := len(lines)
	start := end
	for start > 1 && lines[start-1] != "" {
		start--
	}
	if start <= 1 || start >= end {
		return nil
	}
	var out []trailer
	for _, l := range lines[start:end] {
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			if len(out) == 0 {
				return nil
			}
			continue
		}
		m := trailerRe.FindStringSubmatch(l)
		if m == nil {
			return nil
		}
		out = append(out, trailer{key: m[1], value: strings.TrimSpace(m[2])})
	}
	return out
}

// scanFinalParagraph reads the final paragraph the way git's trailer parser
// does, leniently: every line that is not a continuation and has the shape
// "Key <ws>: value" is returned. block says git would treat the paragraph as a
// trailer block: every line is a trailer line, or there is a Signed-off-by line
// and trailer lines are at least a quarter of the others (git's 25% rule).
func scanFinalParagraph(lines []string) (found []trailer, block bool) {
	end := len(lines)
	start := end
	for start > 1 && lines[start-1] != "" {
		start--
	}
	if start <= 1 || start >= end {
		return nil, false
	}
	var other int
	var signed bool
	for _, l := range lines[start:end] {
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			continue
		}
		m := scanKeyRe.FindStringSubmatch(l)
		if m == nil {
			other++
			continue
		}
		found = append(found, trailer{key: m[1], value: strings.TrimSpace(m[2])})
		if strings.EqualFold(m[1], "Signed-off-by") {
			signed = true
		}
	}
	return found, other == 0 || signed && len(found)*3 >= other
}
