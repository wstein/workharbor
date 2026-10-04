// Package textsafe holds the character classes that must not reach a terminal
// or a log unescaped.
package textsafe

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// IsBidiOrSeparator reports the Unicode bidirectional controls and the line
// and paragraph separators, which reorder or hide text (Trojan Source) or break
// a log line.
func IsBidiOrSeparator(r rune) bool {
	switch {
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == 0x200e || r == 0x200f || r == 0x061c || r == 0x2028 || r == 0x2029
}

// IsControl reports a C0 control character other than tab, DEL, or a C1
// control character (U+0080 to U+009F, among them the one-character CSI).
func IsControl(r rune) bool {
	return (r < 0x20 && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// Escape replaces every control character (IsControl), bidirectional control
// and line separator in s with a visible escape (\n, \r, \x1b, \u009b), and
// keeps tab and every other character. Use it where untrusted text reaches a
// log or a human's terminal (design §7.1, T20).
func Escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 && r != '\t', r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case IsControl(r), IsBidiOrSeparator(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// EscapeJSON rewrites the characters Escape would escape, which JSON encoders
// leave raw (DEL, C1, bidirectional controls; C0 is escaped by the encoder and
// its newlines are the document's own), as \uXXXX in encoded JSON, so
// the document stays valid and means the same but prints inert. Such a
// character occurs only inside a string, where \u is its escape.
func EscapeJSON(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			// A stray invalid byte (a lone 0x9b is a one-byte CSI to some
			// terminals) becomes U+FFFD, as encoding/json does.
			if out == nil {
				out = append(make([]byte, 0, len(b)+16), b[:i]...)
			}
			out = utf8.AppendRune(out, utf8.RuneError)
		case r >= 0x7f && IsControl(r) || IsBidiOrSeparator(r):
			if out == nil {
				out = append(make([]byte, 0, len(b)+16), b[:i]...)
			}
			out = fmt.Appendf(out, `\u%04x`, r)
		case out != nil:
			out = append(out, b[i:i+n]...)
		}
		i += n
	}
	if out == nil {
		return b
	}
	return out
}
