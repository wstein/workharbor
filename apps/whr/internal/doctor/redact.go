// SPDX-License-Identifier: EUPL-1.2

package doctor

import "strings"

// Redactor removes the home directory, user name and host name from text so a
// report can be shared (FR-060). The home directory becomes "~"; the names are
// replaced wherever they occur, except that names shorter than three bytes are
// replaced only as whole path segments, which would otherwise garble the text.
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

func replaceName(s, name, repl string) string {
	if name == "" {
		return s
	}
	if len(name) >= 3 {
		return strings.ReplaceAll(s, name, repl)
	}
	parts := strings.Split(s, "/")
	for i, p := range parts {
		if p == name {
			parts[i] = repl
		}
	}
	return strings.Join(parts, "/")
}

func (r Redactor) value(v any) any {
	switch x := v.(type) {
	case string:
		return r.String(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = r.value(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = r.value(e)
		}
		return out
	}
	return v
}

func (r Redactor) check(c Check) Check {
	c.Message, c.Remediation = r.String(c.Message), r.String(c.Remediation)
	if c.Evidence != nil {
		c.Evidence = r.value(c.Evidence).(map[string]any)
	}
	return c
}
