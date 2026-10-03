// Package textsafe holds the character classes that must not reach a terminal
// or a log unescaped.
package textsafe

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
