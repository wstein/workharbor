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
	assistedRe = regexp.MustCompile(`^[^:\s][^:]*:[A-Za-z0-9][A-Za-z0-9._/+-]*(?: \[[^\]]+\])*$`)
	idRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	botRe      = regexp.MustCompile(`(?i)\[bot\]|\b(?:bot|agent)\b|noreply@anthropic\.com`)

	// depBotRe matches the dependency-update bots, whose generated messages
	// cannot follow every rule: long titles, and a DCO Signed-off-by line.
	depBotRe = regexp.MustCompile(`(?i)^\s*(?:dependabot|renovate)(?:\[bot\])?\b`)
)

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
}

type trailer struct{ key, value string }

// Lint returns one problem per violated rule; an empty result means the
// message is acceptable.
func Lint(msg string, opt Options) []string {
	lines := clean(msg)
	if len(lines) == 0 {
		return []string{"empty commit message"}
	}
	subject := lines[0]
	for _, p := range exemptPrefixes {
		if strings.HasPrefix(subject, p) {
			// These skip the message rules, never the one about origin: a bot's
			// "Merge ..." must not carry a Signed-off-by either.
			var problems []string
			if signoffByBot(lines, opt) {
				problems = append(problems, fmt.Sprintf("Signed-off-by certifies human origin; remove it from commits authored by %q", opt.Author))
			}
			if opt.Final && p != "Merge " && p != "Revert " {
				problems = append(problems, fmt.Sprintf("%q commit is not squashed: run git rebase -i --autosquash before it lands", strings.TrimSpace(p)))
			}
			return problems
		}
	}

	var problems []string
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
	var issues, assisted, tasks, runs, signoffs, changelogs int
	for _, t := range trailers {
		switch {
		case issueKeys[t.key]:
			issues++
			if !issueValue.MatchString(t.value) {
				add("%s: %q is not an issue reference; use #123 or owner/repo#123", t.key, t.value)
			}
		case t.key == "Assisted-by":
			assisted++
			if !assistedRe.MatchString(t.value) {
				add("Assisted-by: %q must be <tool>:<model-id>, e.g. 'Claude Code:claude-sonnet-5-5'", t.value)
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
		case t.key == "Signed-off-by":
			signoffs++
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
	if signoffs > 0 && botRe.MatchString(opt.Author) && !depBot {
		add("Signed-off-by certifies human origin; remove it from commits authored by %q", opt.Author)
	}
	return problems
}

// signoffByBot reports a Signed-off-by trailer on a commit whose author is a bot or
// an agent (the dependency bots are exempt).
func signoffByBot(lines []string, opt Options) bool {
	if !botRe.MatchString(opt.Author) || depBotRe.MatchString(opt.Author) {
		return false
	}
	for _, t := range parseTrailers(lines) {
		if t.key == "Signed-off-by" {
			return true
		}
	}
	return false
}

// clean drops git's comment lines and the scissors section, then trims
// trailing blank lines.
func clean(msg string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(l, "# ------------------------ >8") {
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
