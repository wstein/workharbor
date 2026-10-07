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
	scanKeyRe = regexp.MustCompile(`^([A-Za-z0-9-]+)[ \t]*:[ \t]*(.*)$`)

	// The dependency-update bots, whose generated messages cannot follow every
	// rule: long titles, and a DCO Signed-off-by line. The exemption needs the
	// bot's name and its usual address together. Both are self-asserted, so
	// this only rules out look-alike or accidental names; it proves nothing
	// about who wrote the commit (the forge's PR login does). Patterns are
	// lower case and applied to ASCII-lowered, ASCII-only input.
	depBotIdentities = []struct{ name, email *regexp.Regexp }{
		{ // Dependabot (support@github.com is the address older commits carry)
			regexp.MustCompile(`^dependabot(?:\[bot\])?$`),
			regexp.MustCompile(`^(?:(?:\d+\+)?dependabot\[bot\]@users\.noreply\.github\.com|support@github\.com)$`),
		},
		{ // Renovate
			regexp.MustCompile(`^renovate(?:\[bot\]| bot)?$`),
			regexp.MustCompile(`^(?:(?:\d+\+)?renovate\[bot\]@users\.noreply\.github\.com|bot@renovateapp\.com)$`),
		},
	}
	// authorRe reads "Name <email>", with the optional " <unix-time> <tz>"
	// tail that `git var GIT_AUTHOR_IDENT` appends. Surrounding whitespace of
	// the name is trimmed; the email may hold none.
	authorRe = regexp.MustCompile(`^\s*(.*?)\s*<([^<>\s]*)>(?:\s+\d+\s+[+-]\d{4})?\s*$`)
)

// isDepBot reports an author that is a dependency bot by both name and
// address, compared with ASCII-only case folding.
func isDepBot(author string) bool {
	m := authorRe.FindStringSubmatch(author)
	if m == nil || !isASCII(m[1]) || !isASCII(m[2]) {
		return false
	}
	name, email := strings.ToLower(m[1]), strings.ToLower(m[2])
	for _, id := range depBotIdentities {
		if id.name.MatchString(name) && id.email.MatchString(email) {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

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
	// rules, because they generate their own messages; the exemption needs the bot's
	// own name and address, not a name alone.
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
	// GitTrailers, when GitRead is set, are the "Key: value" lines git itself
	// reports as the trailers of a stored commit (git log
	// --format=%(trailers:only)). A bot author's AI coauthor is then taken from
	// them, since this package's scanner can differ from git's parser; the
	// other rules keep reading the message.
	GitTrailers []string
	GitRead     bool
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
	// What git reads as trailers: in a stored message also what precedes its
	// full cut line, because git cuts there when it reads trailers, while the
	// rest of the message stays text for every other rule.
	views := [][]string{lines}
	if !opt.Scissors {
		if cut := cleanWith(msg, func(l string) bool { return l == gitCutLine }); len(cut) != len(lines) {
			views = append(views, cut)
		}
	}
	var scans []finalParagraph
	for _, v := range views {
		scans = append(scans, scanFinalParagraph(v))
	}
	attributionProblems := attributionProblemsFor(scans, opt)
	for _, p := range exemptPrefixes {
		if strings.HasPrefix(subject, p) {
			// These skip the message rules, never the one about origin: a bot's
			// "Merge ..." must not carry a Signed-off-by either.
			problems := attributionProblems
			if signoffByBot(scans, opt) {
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
	depBot := isDepBot(opt.Author)
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
	if signoffByBot(scans, opt) {
		add("Signed-off-by certifies human origin; remove it from commits authored by %q", opt.Author)
	}
	return problems
}

// attributionProblemsFor validates both legacy assistance and current coauthor
// trailers, including on subject-exempt commits. Human coauthors are independent
// of AI attribution on a human's commit; a bot or agent author takes none.
// Reserved project addresses identify AI attribution; names alone may belong
// to humans. The message cannot prove which model actually ran. Attribution is
// validated where git reads a trailer block, and a bot or agent author must
// have at least one valid AI coauthor in it; a person coauthor on a bot's
// commit is refused on any line that has the shape of a trailer, so no
// difference between git's reading and this one can hide it.
func attributionProblemsFor(scans []finalParagraph, opt Options) []string {
	botAuthor := botRe.MatchString(opt.Author) && !isDepBot(opt.Author)
	var problems []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			problems = append(problems, p)
		}
	}
	person := func(t trailer) {
		add(fmt.Sprintf("Co-Authored-By %q is a person; a commit authored by %q (a bot or agent) takes only AI attribution coauthors", t.value, opt.Author))
	}
	hasAI := false
	for i, sc := range scans {
		gitView := i == len(scans)-1 // the last scan is what git reads
		for _, t := range sc.block {
			if strings.EqualFold(t.key, "Assisted-by") && !assistedRe.MatchString(t.value) {
				add(fmt.Sprintf("Assisted-by: %q must be <tool>:<model-id>", t.value))
			}
			if !strings.EqualFold(t.key, "Co-Authored-By") {
				continue
			}
			m := coauthorRe.FindStringSubmatch(t.value)
			if m == nil {
				add("Co-Authored-By must be Name <email>; AI attribution requires <tool> <model-id> <attribution-email>")
				continue
			}
			name, email := m[1], strings.ToLower(m[2])
			fields := strings.Fields(name)
			id, ok := aiIdentities[email]
			if !ok {
				continue // Other identities may be legitimate human coauthors of a human's commit.
			}
			if !strings.EqualFold(fields[0], id.vendor) || !modelNameRe.MatchString(strings.Join(fields[1:], " ")) {
				add(fmt.Sprintf("Co-Authored-By AI attribution requires %s <model-id> <%s>; use the model name as exposed by the session or unknown", id.vendor, email))
			}
			if gitView && !opt.GitRead {
				hasAI = true
			}
		}
		if !botAuthor {
			continue
		}
		for _, t := range sc.all {
			if !strings.EqualFold(t.key, "Co-Authored-By") {
				continue
			}
			if m := coauthorRe.FindStringSubmatch(t.value); m != nil {
				if _, ok := aiIdentities[strings.ToLower(m[2])]; ok {
					continue
				}
			}
			person(t)
		}
	}
	for _, l := range opt.GitTrailers {
		if m := scanKeyRe.FindStringSubmatch(l); m != nil && strings.EqualFold(m[1], "Co-Authored-By") && isAICoauthor(m[2]) {
			hasAI = true
		}
	}
	if botAuthor && !hasAI {
		add(fmt.Sprintf("a commit authored by %q (a bot or agent) needs an AI Co-Authored-By trailer: <tool> <model-id> <attribution-email>", opt.Author))
	}
	return problems
}

// isAICoauthor reports a Co-Authored-By value that is a well-formed AI
// attribution on a reserved address.
func isAICoauthor(value string) bool {
	m := coauthorRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return false
	}
	id, ok := aiIdentities[strings.ToLower(m[2])]
	fields := strings.Fields(m[1])
	return ok && strings.EqualFold(fields[0], id.vendor) && modelNameRe.MatchString(strings.Join(fields[1:], " "))
}

// signoffByBot reports a Signed-off-by line, in any shape git reads as a
// trailer, on a commit whose author is a bot or an agent (the dependency bots
// are exempt).
func signoffByBot(scans []finalParagraph, opt Options) bool {
	if !botRe.MatchString(opt.Author) || isDepBot(opt.Author) {
		return false
	}
	for _, sc := range scans {
		for _, t := range sc.all {
			if strings.EqualFold(t.key, "Signed-off-by") {
				return true
			}
		}
	}
	return false
}

// gitCutLine is the line git's own scissors cleanup cuts at.
const gitCutLine = "# ------------------------ >8 ------------------------"

// clean drops git's comment lines and, when scissors is set (the commit-msg
// hook, where git has cleaned the message up the same way), the scissors
// section, then trims trailing blank lines.
func clean(msg string, scissors bool) []string {
	return cleanWith(msg, func(l string) bool { return scissors && strings.HasPrefix(l, "# ------------------------ >8") })
}

// cleanWith drops comment lines and everything from the first line cut says
// to cut at. cut sees the raw line: git cuts at its exact cut line only, so a
// trailing space, tab or carriage return makes a line text. Whitespace at the
// end of a line (git's blank, too: space, tab and carriage return) is trimmed.
func cleanWith(msg string, cut func(line string) bool) []string {
	var out []string
	for _, l := range strings.Split(msg, "\n") {
		if cut(l) {
			break
		}
		if strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t\r"))
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

// finalParagraph is the last paragraph of a message as git's trailer parser
// reads it. all holds every line of it that has the shape "Key <ws>: value",
// whatever git makes of the paragraph, with leading whitespace dropped, so a
// line git folds into the one above is seen too. block holds the trailers git
// reads: none unless git takes the paragraph for a trailer block.
type finalParagraph struct{ all, block []trailer }

// Git's recognised prefixes: a line starting with one of them makes the
// paragraph a trailer block even when prose shares it.
var gitPrefixes = []string{"Signed-off-by: ", "(cherry picked from commit "}

// scanFinalParagraph reads the final paragraph the way git does, walking up
// from its last line: a line starting with whitespace belongs to the line above
// it; a line with a token, optional whitespace and a colon (the token may begin
// with a digit or dash) is a trailer, as is a recognised cherry-pick line; any
// other line is prose. The paragraph is a block, and every trailer of it
// counts, when it holds trailers and no prose, or a recognised prefix and
// trailers at least a quarter of the prose (git's 25% rule, applied once to
// the whole paragraph).
func scanFinalParagraph(lines []string) finalParagraph {
	end := len(lines)
	start := end
	for start > 1 && lines[start-1] != "" {
		start--
	}
	if start <= 1 || start >= end {
		return finalParagraph{}
	}
	var fp finalParagraph
	for _, l := range lines[start:end] {
		if m := scanKeyRe.FindStringSubmatch(strings.TrimLeft(l, " \t\r")); m != nil {
			fp.all = append(fp.all, trailer{key: m[1], value: strings.TrimSpace(m[2])})
		}
	}
	var read []trailer
	var trailers, prose, pending int
	var recognised bool
	for i := end - 1; i >= start; i-- {
		l := lines[i]
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "\r") {
			pending++
			continue
		}
		m := scanKeyRe.FindStringSubmatch(l)
		prefixed := false
		for _, p := range gitPrefixes {
			prefixed = prefixed || strings.HasPrefix(l, p)
		}
		recognised = recognised || prefixed
		if m != nil || prefixed {
			trailers++
			pending = 0
			if m != nil {
				read = append(read, trailer{key: m[1], value: strings.TrimSpace(m[2])})
			}
		} else {
			prose += 1 + pending
			pending = 0
		}
	}
	prose += pending // continuation lines with no line above them
	if trailers > 0 && (prose == 0 || recognised && trailers*3 >= prose) {
		fp.block = reverse(read)
	}
	return fp
}

func reverse(ts []trailer) []trailer {
	for i, j := 0, len(ts)-1; i < j; i, j = i+1, j-1 {
		ts[i], ts[j] = ts[j], ts[i]
	}
	return ts
}
