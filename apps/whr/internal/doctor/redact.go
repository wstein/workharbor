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

// replaceName replaces name where it is a whole path word: delimited by the
// ends of the text, "/", "-" or "_". A user called "doc" therefore never
// alters "doctor", and a user called "docker" never alters "docker.sock".
func replaceName(s, name, repl string) string {
	if name == "" {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, name)
		if i < 0 {
			break
		}
		end := i + len(name)
		if (i > 0 && !delim(s[i-1])) || (end < len(s) && !delim(s[end])) {
			b.WriteString(s[:i+1])
			s = s[i+1:]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString(repl)
		s = s[end:]
	}
	b.WriteString(s)
	return b.String()
}

func delim(c byte) bool { return c == '/' || c == '-' || c == '_' }

// pathKeys are the evidence keys that hold file system paths; only their
// values are redacted (FR-060: names "in a path"). executableKeys name paths
// found through PATH, where only the home directory is replaced.
var (
	pathKeys       = map[string]bool{"path": true, "checked": true}
	executableKeys = map[string]bool{"executable": true}
)

func (r Redactor) value(key string, v any) any {
	switch x := v.(type) {
	case string:
		switch {
		case pathKeys[key]:
			return r.String(x)
		case executableKeys[key] && r.home != "":
			return replaceHome(x, r.home)
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
