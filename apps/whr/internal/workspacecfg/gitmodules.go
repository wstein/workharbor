// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxGitmodulesEntries is the number of submodule entries FR-055 admits.
const MaxGitmodulesEntries = 256

// smEntry is one [submodule "name"] section with its path and url.
type smEntry struct {
	name, path, url string
}

var (
	sectionLine = regexp.MustCompile(`^\[submodule "((?:[^"\\]|\\.)*)"\][ \t]*(?:[#;].*)?$`)
	keyLine     = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*)[ \t]*(?:=(.*))?$`)
)

// gitmodulesResult is the outcome of reading the .gitmodules text.
type gitmodulesResult struct {
	entries []smEntry
	// usable is false when the text cannot be trusted for entry-based checks.
	usable bool
	// updates and unsupported are keys found beyond path and url, by entry.
	extra []smExtra
	dups  []string
}

type smExtra struct {
	section, key, value string
}

// parseGitmodules reads the bounded INI subset of .gitmodules in process; no
// `git config` call and no include ever touches it. Problems are findings.
func parseGitmodules(blob []byte, c *collector) gitmodulesResult {
	res := gitmodulesResult{}
	bad := func(msg string) gitmodulesResult {
		c.add("gitmodules.syntax", ".gitmodules", msg, "")
		return gitmodulesResult{}
	}
	switch {
	case len(blob) > MaxGitmodulesBytes:
		c.add("gitmodules.too_large", ".gitmodules", ".gitmodules is larger than its limit", "")
		return res
	case bytes.IndexByte(blob, 0) >= 0:
		return bad(".gitmodules contains a NUL byte")
	case !utf8.Valid(blob):
		return bad(".gitmodules is not valid UTF-8")
	}
	type section struct {
		name            string
		path, url       string
		hasPath, hasURL bool
	}
	var secs []*section
	var cur *section
	names := map[string]bool{}
	lines := strings.Split(string(blob), "\n")
	for n, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';':
			continue
		case trimmed[0] == '[':
			m := sectionLine.FindStringSubmatch(trimmed)
			if m == nil {
				return bad("line " + strconv.Itoa(n+1) + " is not a [submodule \"name\"] header")
			}
			name := unescapeSection(m[1])
			if names[name] {
				c.add("gitmodules.duplicate_field", ".gitmodules", "duplicate section for submodule "+quoteKey(name), "")
			}
			names[name] = true
			cur = &section{name: name}
			secs = append(secs, cur)
			if len(secs) > MaxGitmodulesEntries {
				c.add("gitmodules.too_large", ".gitmodules", ".gitmodules has more than 256 entries", "")
				return res
			}
		default:
			if cur == nil {
				return bad("line " + strconv.Itoa(n+1) + " is outside a section")
			}
			m := keyLine.FindStringSubmatch(trimmed)
			if m == nil || !strings.Contains(trimmed, "=") {
				return bad("line " + strconv.Itoa(n+1) + " is not a key = value pair")
			}
			val, ok := parseValue(m[2])
			if !ok {
				return bad("line " + strconv.Itoa(n+1) + " has an unsupported value syntax")
			}
			key := strings.ToLower(m[1])
			switch key {
			case "path":
				if cur.hasPath {
					c.add("gitmodules.duplicate_field", ".gitmodules", "submodule "+quoteKey(cur.name)+" repeats path", "")
				} else {
					cur.path, cur.hasPath = val, true
				}
			case "url":
				if cur.hasURL {
					c.add("gitmodules.duplicate_field", ".gitmodules", "submodule "+quoteKey(cur.name)+" repeats url", "")
				} else {
					cur.url, cur.hasURL = val, true
				}
			default:
				res.extra = append(res.extra, smExtra{cur.name, key, val})
			}
		}
	}
	for _, s := range secs {
		if !s.hasPath || !s.hasURL {
			return bad("submodule " + quoteKey(s.name) + " lacks a path or a url")
		}
		res.entries = append(res.entries, smEntry{s.name, s.path, s.url})
	}
	res.usable = true
	return res
}

func unescapeSection(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseValue reads the right-hand side of `key = value`: optional quotes,
// the escapes \\ \" \n \t \b, and a trailing # or ; comment outside quotes.
// A backslash continuation or any other escape is unsupported.
func parseValue(rhs string) (string, bool) {
	s := strings.TrimLeft(rhs, " \t")
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\\':
			i++
			if i >= len(s) {
				return "", false // continuation
			}
			switch s[i] {
			case '\\', '"':
				b.WriteByte(s[i])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			default:
				return "", false
			}
		case ch == '"':
			inQuote = !inQuote
		case !inQuote && (ch == '#' || ch == ';'):
			return strings.TrimRight(b.String(), " \t"), true
		default:
			b.WriteByte(ch)
		}
	}
	if inQuote {
		return "", false
	}
	return strings.TrimRight(b.String(), " \t"), true
}

var (
	httpsSubmodule = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(?::[0-9]{1,5})?/[A-Za-z0-9._~!$&'()*+,;=:@%/-]+$`)
	sshSubmodule   = regexp.MustCompile(`^ssh://(?:[A-Za-z0-9._-]+@)?[A-Za-z0-9.-]+(?::[0-9]{1,5})?/[A-Za-z0-9._~!$&'()*+,;=:@%/-]+$`)
	scpSubmodule   = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._~!$&'()*+,;=:@%/-]+$`)
	scpPassword    = regexp.MustCompile(`^[^@/:\s]+:[^@/\s]*@[^/:\s]+:`)
)

// submoduleURLCode classifies a .gitmodules URL: "" when admitted, otherwise
// the finding code. The URL itself is never echoed.
func submoduleURLCode(u string) string {
	if u != "" && !strings.ContainsAny(u, " \t\r\n\x00") {
		if urlHasCredentials(u) || scpPassword.MatchString(u) {
			return "submodule.url_credentials"
		}
	}
	switch {
	case u == "" || len(u) > maxURL || strings.ContainsFunc(u, func(r rune) bool { return r < 0x21 || r == 0x7f }):
		return "submodule.url_invalid"
	case !(httpsSubmodule.MatchString(u) || sshSubmodule.MatchString(u) || scpSubmodule.MatchString(u)):
		return "submodule.url_invalid"
	case !wellFormedPercent(u):
		return "submodule.url_invalid"
	case strings.HasPrefix(u, "-"):
		return "submodule.url_invalid"
	}
	if _, userinfo, host, _, ok := urlAuthority(u); ok {
		if strings.HasPrefix(userinfo, "-") || strings.HasPrefix(host, "-") {
			return "submodule.url_invalid"
		}
	} else if i := strings.Index(u, "@"); i >= 0 && strings.HasPrefix(u[i+1:], "-") {
		return "submodule.url_invalid"
	}
	return ""
}
