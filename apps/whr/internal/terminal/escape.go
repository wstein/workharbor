// SPDX-License-Identifier: EUPL-1.2

// Package terminal makes untrusted text inert before whr prints it (FR-022).
//
// The stateful stream filter for relayed sessions (FR-022a) is a separate work
// item (T014) and is not part of this package yet.
package terminal

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsUnsafe reports whether r belongs to a class FR-022 replaces in human
// output and escapes in JSON: C0 controls except tab, DEL,
// C1 controls, bidirectional controls, the Unicode line and paragraph
// separators, and every other format character (category Cf).
func IsUnsafe(r rune) bool {
	switch {
	case r == '\t':
		return false
	case r < 0x20, r >= 0x7f && r <= 0x9f:
		return true
	case r == 0x2028, r == 0x2029, r == 0x061c, r >= 0x200e && r <= 0x200f,
		r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return unicode.Is(unicode.Cf, r)
}

// Escape replaces every unsafe rune of s with a visible
// placeholder: <U+XXXX> for a rune and <0xNN> for each byte of invalid UTF-8.
// The result contains no terminal control and no way to forge a new line.
func Escape(s string) string {
	if clean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteString("<0x")
			b.WriteString(hex(uint64(s[i]), 2))
			b.WriteByte('>')
		case IsUnsafe(r):
			b.WriteString("<U+")
			b.WriteString(hex(uint64(r), 4))
			b.WriteByte('>')
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func clean(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size == 1) || IsUnsafe(r) {
			return false
		}
		i += size
	}
	return true
}

func hex(v uint64, width int) string {
	h := strings.ToUpper(strconv.FormatUint(v, 16))
	if len(h) < width {
		h = strings.Repeat("0", width-len(h)) + h
	}
	return h
}

// EscapeJSON rewrites the output of encoding/json so that no unsafe rune
// appears raw: each becomes a \uXXXX escape (surrogate pairs above the BMP).
// encoding/json already escapes C0 controls and U+2028/U+2029; this covers DEL,
// C1, bidirectional and other format characters. Such runes only occur inside
// JSON strings, so a rune-wise rewrite cannot change the document structure.
func EscapeJSON(doc []byte) []byte {
	out := make([]byte, 0, len(doc))
	for i := 0; i < len(doc); {
		r, size := utf8.DecodeRune(doc[i:])
		if r == utf8.RuneError && size <= 1 || !IsUnsafe(r) {
			out = append(out, doc[i:i+size]...)
			i += size
			continue
		}
		if r > 0xffff {
			hi, lo := utf16Pair(r)
			out = appendU(appendU(out, hi), lo)
		} else {
			out = appendU(out, r)
		}
		i += size
	}
	return out
}

func utf16Pair(r rune) (rune, rune) {
	r -= 0x10000
	return 0xd800 + (r>>10)&0x3ff, 0xdc00 + r&0x3ff
}

func appendU(b []byte, r rune) []byte {
	b = append(b, '\\', 'u')
	h := strings.ToLower(hex(uint64(r), 4))
	return append(b, h...)
}
