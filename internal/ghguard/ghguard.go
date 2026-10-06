// Package ghguard refuses Bash commands that look like a GitHub GraphQL call
// through `gh` (#314, decision H74:c). It is deliberately NOT a shell parser:
// it normalises the command text and searches it, and it over-blocks rather
// than tries to understand shell syntax. The settings.json deny rules stay as
// defence in depth.
package ghguard

import (
	"regexp"
	"strings"
)

const maxCommand = 32 << 10

// Check returns a non-empty reason when command must be refused.
//
// The rule. Normalise the text (lower case, percent-decoded up to three
// rounds, quotes, backslashes, backticks, `^` and `+` removed). Refuse when
//
//  1. the normalised text contains "graphql" and contains `gh` as a word
//     (also `=gh`, `/opt/homebrew/bin/gh`, `"gh"`, `GH`) or `api` as a word; or
//  2. it contains `gh` or `api` as a word, a dynamic piece (`$`, backtick,
//     `{`, `*`, `?`, `[`) and a graphql-ish piece ("graph" or "ql"), which
//     catches `gr$(echo a)phql`, `graph{q..q}l`, `$G api gr$Xql`; or
//  3. the command is longer than 32 KiB (bounded work).
//
// Allowed even when "graphql" is present: (a) a plain one-line
// `gh <issue|pr|search|repo|run|workflow|label|release|...> ...` with none of
// ; & | < > $ ` ( ) { } \ * ? [ and no newline (`gh issue list --search
// graphql`); (b) data contexts, removed before the search: a single-quoted
// argument of git commit -m/--message or gh pr|issue create|comment|edit
// -b/--body/-t/--title, and a `<<'EOF'` heredoc body owned by cat, tee,
// git commit or gh pr|issue with nothing else on its line (also inside
// `"$(cat <<'EOF'`). The data exception is dropped when the remaining text
// has a stray single quote, a shell or interpreter word (sh, bash, eval, source, python, xargs, ...) or an odd number of double quotes. Anything without
// "graphql" is allowed, so `gh api repos/...` and scripts/board-snapshot.sh
// (graphql lives in the script file, not in the command text) pass.
//
// Known over-blocks: any command that merely mentions `gh api graphql` or the
// word api next to graphql outside the allowed contexts (echo, grep, a
// double-quoted commit message; workaround: -F file or single quotes),
// `curl .../graphql` (the `api.github.com` host is an `api` word), and
// dynamic constructs with "ql" in them.
//
// Residual gaps (not covered): commands assembled at runtime or read from
// files (`xargs < list`, `$(cat f)`, `gh api $EP`, `gh api *`, `gr*`), gh or
// shell aliases and functions, `gh api --input` bodies, other clients without
// gh or api words, a quote not tracked across lines in the data exception, a
// hook that fails to start or times out (Claude Code treats that as allow),
// a hook compiled by `go run` from the working tree (an agent editing
// internal/ghguard, go.mod or GOFLAGS can turn it into allow), matcher scope
// (only Bash, Monitor, PowerShell; other tools are not inspected) and runtime
// enforcement, which is unverified until the #274-style probe is run.
func Check(command string) string {
	if len(command) > maxCommand {
		return "command too large to inspect (#314)"
	}
	if !risky(command) {
		return ""
	}
	if simpleGh(command) {
		return ""
	}
	if s, ok := stripData(command); ok && !risky(s) {
		return ""
	}
	return "GraphQL via gh is refused for agent sessions (#314): use REST (gh api repos/...) or scripts/board-snapshot.sh"
}

var (
	ghWord  = regexp.MustCompile(`(^|[^a-z0-9_.-])gh($|[^a-z0-9_-])`)
	apiWord = regexp.MustCompile(`(^|[^a-z0-9_-])api($|[^a-z0-9_-])`)
	dropper = strings.NewReplacer("'", "", `"`, "", `\`, "", "`", "", "^", "", "+", "", "\x00", "")
)

func normalise(s string) string {
	for range 3 {
		d := unpercent(s)
		if d == s {
			break
		}
		s = d
	}
	return dropper.Replace(strings.ToLower(s))
}

func unpercent(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c >= 'a':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// risky is rule 1 or 2 on the normalised text.
func risky(s string) bool {
	n := normalise(s)
	if !ghWord.MatchString(n) && !apiWord.MatchString(n) {
		return false
	}
	if strings.Contains(n, "graphql") {
		return true
	}
	return strings.ContainsAny(n, "${*?[`") && (strings.Contains(n, "graph") || strings.Contains(n, "ql")) ||
		strings.ContainsAny(s, "`") && (strings.Contains(n, "graph") || strings.Contains(n, "ql"))
}

var ghSub = map[string]bool{
	"issue": true, "pr": true, "search": true, "repo": true, "run": true,
	"workflow": true, "label": true, "release": true, "status": true, "browse": true, "gist": true,
}

// simpleGh: one plain line `gh <non-api subcommand> ...` with no shell metacharacters.
func simpleGh(c string) bool {
	if strings.ContainsAny(c, ";&|<>$`(){}\\*?[\n\r") {
		return false
	}
	f := strings.Fields(c)
	if len(f) < 2 || !ghSub[f[1]] {
		return false
	}
	return f[0] == "gh" || strings.HasSuffix(f[0], "/gh")
}

var singleQuoted = regexp.MustCompile("(?m)(^|&&\\s*|;\\s*)((?:git commit|gh (?:pr|issue) (?:create|comment|edit))" +
	"(?:[^'\"`$\\\\;&|<>()\\n]*?(?:-[a-zA-Z]*m|--message|-b|--body|-t|--title)[ =]'[^']*')+)")

// shellWord: a heredoc may be consumed by a shell elsewhere in the text.
var (
	shellWord   = regexp.MustCompile(`(^|[^a-z0-9_-])(ba|z|da|k|c|tc|fi)?sh|eval|source|python3?|perl|node|ruby|xargs|exec($|[^a-z0-9_-])`)
	innerQuoted = regexp.MustCompile("'[^']*'")
)

var (
	ownerCat = regexp.MustCompile("^(cat(\\s*>>?\\s*[^\\s|;&<>$`()]+)?|tee(\\s+[^|;&<>$`()]+)?)$")
	ownerGit = regexp.MustCompile("^(git commit|gh (pr|issue) (create|comment|edit))(\\s[^|;&<>$`()]*?)?(\"?\\$\\(\\s*cat)?$")
	tagRe    = regexp.MustCompile(`^<<(-?)'([A-Za-z_][A-Za-z0-9_]*)'[ \t]*\n`)
)

// stripData removes the allowed data contexts. ok is false when nothing was
// removed or the leftover has unbalanced quotes (then no exception applies).
func stripData(c string) (string, bool) {
	changed := false
	// heredocs
	var out strings.Builder
	rest := c
	for {
		i := strings.Index(rest, "<<")
		if i < 0 {
			break
		}
		m := tagRe.FindStringSubmatch(rest[i:])
		if m == nil {
			out.WriteString(rest[:i+2])
			rest = rest[i+2:]
			continue
		}
		prefix := out.String() + rest[:i]
		ls := strings.LastIndex(prefix, "\n") + 1
		owner := strings.TrimSpace(prefix[ls:])
		bodyStart := i + len(m[0])
		end := -1
		pos := bodyStart
		for pos <= len(rest) {
			nl := strings.IndexByte(rest[pos:], '\n')
			line, next := rest[pos:], len(rest)+1
			if nl >= 0 {
				line, next = rest[pos:pos+nl], pos+nl+1
			}
			if m[1] == "-" {
				line = strings.TrimLeft(line, "\t")
			}
			if line == m[2] {
				end = min(next-1, len(rest))
				break
			}
			if nl < 0 {
				break
			}
			pos = next
		}
		if end < 0 || (!ownerCat.MatchString(owner) && !ownerGit.MatchString(owner)) {
			out.WriteString(rest[:i+2])
			rest = rest[i+2:]
			continue
		}
		out.WriteString(rest[:i])
		out.WriteString("<<X")
		rest = rest[end:]
		changed = true
	}
	out.WriteString(rest)
	s := out.String()
	if t := singleQuoted.ReplaceAllStringFunc(s, func(x string) string {
		changed = true
		return innerQuoted.ReplaceAllString(x, "X")
	}); t != s {
		s = t
	}
	if !changed || shellWord.MatchString(strings.ToLower(s)) || strings.Contains(s, "'") || strings.Count(s, `"`)%2 != 0 {
		return "", false
	}
	return s, true
}
