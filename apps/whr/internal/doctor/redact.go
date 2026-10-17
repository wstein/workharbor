// SPDX-License-Identifier: EUPL-1.2

package doctor

import "strings"

// Redactor removes the home directory, user name and host name from text so a
// report can be shared (FR-060). The home directory becomes "~"; the names are
// replaced where they occur as a word of a path (see replaceName).
type Redactor struct{ home, user, host string }

// NewRedactor returns a redactor; empty values are ignored.
func NewRedactor(home, user, host string) Redactor {
	home = strings.TrimRight(home, "/")
	return Redactor{home: home, user: user, host: host}
}

// String redacts s.
func (r Redactor) String(s string) string {
	if r.home != "" {
		s = replaceHome(s, r.home)
	}
	s = replaceName(s, r.user, "<user>")
	return replaceName(s, r.host, "<host>")
}

func replaceHome(s, home string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, home)
		if i < 0 {
			break
		}
		end := i + len(home)
		if end < len(s) && s[end] != '/' && !strings.ContainsAny(s[end:end+1], " \t\n\"',;:)") {
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

// replaceName replaces name, compared without regard to ASCII case, where it is
// a whole word of a path: delimited by the ends of the text or by any byte that
// is not an ASCII letter, digit or part of a non-ASCII character. So "doc"
// never alters "doctor", while "werner.data", "x@werner" and "Werner" are
// replaced. Names in systemWords are never replaced.
func replaceName(s, name, repl string) string {
	if name == "" || systemWords[strings.ToLower(name)] {
		return s
	}
	ls, ln := foldASCII(s), foldASCII(name)
	var b strings.Builder
	pos := 0
	for {
		i := strings.Index(ls[pos:], ln)
		if i < 0 {
			break
		}
		i += pos
		end := i + len(ln)
		if (i > 0 && wordByte(s[i-1])) || (end < len(s) && wordByte(s[end])) {
			b.WriteString(s[pos : i+1])
			pos = i + 1
			continue
		}
		b.WriteString(s[pos:i])
		b.WriteString(repl)
		pos = end
	}
	b.WriteString(s[pos:])
	return b.String()
}

func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func wordByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

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
			if i := strings.LastIndex(x, "/"); i >= 0 {
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
