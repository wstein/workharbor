// SPDX-License-Identifier: EUPL-1.2

package doctor

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Redactor removes the home directory, user name and host name from text so a
// report can be shared (FR-060). The home directory becomes "~"; the names are
// replaced where they occur as a word of a path (see replaceName).
type Redactor struct{ home, user, host string }

// NewRedactor returns a redactor; empty values are ignored.
func NewRedactor(home, user, host string) Redactor {
	home = strings.TrimRight(home, `/\`)
	return Redactor{home: home, user: user, host: host}
}

// String redacts s.
func (r Redactor) String(s string) string {
	if r.home != "" {
		s = replaceHome(s, r.home)
	}
	return replaceNames(s, []nameRepl{{r.user, "<user>"}, {r.host, "<host>"}})
}

func replaceHome(s, home string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, home)
		if i < 0 {
			break
		}
		end := i + len(home)
		if end < len(s) && s[end] != '/' && s[end] != '\\' && !strings.ContainsAny(s[end:end+1], " \t\n\"',;:)") {
			b.WriteString(s[:end])
			s = s[end:]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString("~")
		s = s[end:]
	}
	b.WriteString(s)
	return b.String()
}

// systemWords are standard path components and tool names. A user or host
// called one of these is not redacted: it would corrupt ordinary paths
// (/var/run/docker.sock, /usr/bin/git) and says nothing about the person.
// This is a deliberate tradeoff of FR-060's "wherever they occur in a path".
var systemWords = map[string]bool{
	"run": true, "bin": true, "sbin": true, "usr": true, "var": true, "tmp": true, "git": true, "root": true,
	"home": true, "etc": true, "opt": true, "srv": true, "docker": true, "lib": true, "lib64": true, "dev": true,
	"proc": true, "sys": true, "mnt": true, "media": true, "local": true, "share": true, "boot": true,
}

// SystemWords lists the names that are never redacted, sorted.
func SystemWords() []string {
	out := make([]string, 0, len(systemWords))
	for w := range systemWords {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

type nameRepl struct{ name, repl string }

// replaceNames replaces the names in one pass, so a placeholder is never
// rescanned (a host called "user" cannot garble "<user>"). A name matches,
// without regard to Unicode case, where it is a whole word of a path:
// delimited by the ends of the text or by any rune that is not a letter or a
// digit. "doc" therefore never alters "doctor", while "werner.data", "x@werner"
// and "Werner" are replaced. Names in systemWords are never replaced.
func replaceNames(s string, names []nameRepl) string {
	var active []nameRepl
	for _, n := range names {
		if n.name != "" && !systemWords[strings.ToLower(n.name)] {
			active = append(active, n)
		}
	}
	if len(active) == 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		// The longest matching name wins, so "werner" never cuts into "werner-pc".
		best, bestEnd := "", -1
		for _, n := range active {
			end, ok := matchFold(s, i, n.name)
			if !ok || end <= bestEnd {
				continue
			}
			if i > 0 {
				if r, _ := utf8.DecodeLastRuneInString(s[:i]); isWordRune(r) {
					continue
				}
			}
			if end < len(s) {
				if r, _ := utf8.DecodeRuneInString(s[end:]); isWordRune(r) {
					continue
				}
			}
			best, bestEnd = n.repl, end
		}
		if bestEnd >= 0 {
			b.WriteString(best)
			i = bestEnd
			continue
		}
		// Copy the original bytes, valid UTF-8 or not.
		_, w := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+w])
		i += w
	}
	return b.String()
}

// matchFold reports whether s at byte offset i starts with name, comparing
// rune by rune under Unicode simple case folding, and returns the end offset.
func matchFold(s string, i int, name string) (int, bool) {
	for _, nr := range name {
		if i >= len(s) {
			return 0, false
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && w == 1 || !foldEqual(r, nr) {
			return 0, false
		}
		i += w
	}
	return i, true
}

func foldEqual(a, b rune) bool {
	for f := unicode.SimpleFold(a); ; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
		if f == a {
			return a == b
		}
	}
}

// isWordRune counts letters, digits and combining marks as part of a word.
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) }

// pathKeys are the evidence keys that hold file system paths; only their
// values are redacted (FR-060: names "in a path"). Constant paths such as the
// daemon.json location ("checked") are not redacted. executableKeys name a
// program found through PATH: the home directory and the names in its
// directories are redacted, its final component (the program) is not.
var (
	pathKeys       = map[string]bool{"path": true}
	executableKeys = map[string]bool{"executable": true}
)

func (r Redactor) value(key string, v any) any {
	switch x := v.(type) {
	case string:
		switch {
		case pathKeys[key]:
			return r.String(x)
		case executableKeys[key]:
			if i := strings.LastIndexAny(x, `/\`); i >= 0 {
				return r.String(x[:i+1]) + x[i+1:]
			}
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = r.value(key, x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = r.value(k, e)
		}
		return out
	}
	return v
}

// check redacts the path values of the evidence of c.
func (r Redactor) check(c Check) Check {
	if c.Evidence != nil {
		c.Evidence = r.value("", c.Evidence).(map[string]any)
	}
	return c
}
