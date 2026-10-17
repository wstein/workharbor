// SPDX-License-Identifier: EUPL-1.2

package ptytest

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type tokenKind int

const (
	tokText       tokenKind = iota // one printable rune (or invalid byte)
	tokControl                     // C0 control byte
	tokCSI                         // ESC [ params final
	tokOther                       // OSC, other escapes, C1 controls: ignored
	tokIncomplete                  // truncated escape sequence at end of input
)

type token struct {
	kind   tokenKind
	n      int // bytes consumed
	r      rune
	params []int
	final  byte
}

// next splits the first token off b (len(b) > 0).
func next(b []byte) token {
	c := b[0]
	switch {
	case c == 0x1b:
		return escape(b)
	case c < 0x20 || c == 0x7f:
		return token{kind: tokControl, n: 1, r: rune(c)}
	case c < 0x80:
		return token{kind: tokText, n: 1, r: rune(c)}
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError && n <= 1 {
		if !utf8.FullRune(b) {
			return token{kind: tokIncomplete, n: len(b)}
		}
		return token{kind: tokText, n: 1, r: utf8.RuneError}
	}
	if r >= 0x80 && r <= 0x9f {
		return token{kind: tokOther, n: n}
	}
	return token{kind: tokText, n: n, r: r}
}

func escape(b []byte) token {
	if len(b) < 2 {
		return token{kind: tokIncomplete, n: 1}
	}
	switch b[1] {
	case '[':
		i := 2
		for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
			i++
		}
		for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
			i++
		}
		if i >= len(b) {
			return token{kind: tokIncomplete, n: len(b)}
		}
		if b[i] < 0x40 || b[i] > 0x7e {
			return token{kind: tokOther, n: i} // malformed: drop the prefix
		}
		return token{kind: tokCSI, n: i + 1, params: parseParams(string(b[2:i])), final: b[i]}
	case ']', 'P', 'X', '^', '_': // string sequences end at BEL or ST
		for i := 2; i < len(b); i++ {
			if b[i] == 0x07 {
				return token{kind: tokOther, n: i + 1}
			}
			if b[i] == 0x1b {
				if i+1 >= len(b) {
					return token{kind: tokIncomplete, n: len(b)}
				}
				if b[i+1] == '\\' {
					return token{kind: tokOther, n: i + 2}
				}
			}
		}
		return token{kind: tokIncomplete, n: len(b)}
	}
	i := 1
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f { // intermediates
		i++
	}
	if i >= len(b) {
		return token{kind: tokIncomplete, n: len(b)}
	}
	return token{kind: tokOther, n: i + 1}
}

func parseParams(s string) []int {
	s = strings.TrimLeft(s, "?<=>")
	if s == "" {
		return nil
	}
	var out []int
	for _, f := range strings.Split(s, ";") {
		v, _ := strconv.Atoi(f)
		out = append(out, v)
	}
	return out
}

// project returns the visible text of b with every control character and
// escape sequence removed except newline and tab, and, for each byte of that
// text, the offset in b just after the token that produced it. A truncated
// trailing sequence is left unconsumed.
func project(b []byte) (plain []byte, ends []int) {
	for off := 0; off < len(b); {
		t := next(b[off:])
		if t.kind == tokIncomplete {
			break
		}
		off += t.n
		switch {
		case t.kind == tokText:
			var tmp [4]byte
			k := utf8.EncodeRune(tmp[:], t.r)
			if t.r == utf8.RuneError {
				k = copy(tmp[:], b[off-t.n:off])
			}
			for i := 0; i < k; i++ {
				plain = append(plain, tmp[i])
				ends = append(ends, off)
			}
		case t.kind == tokControl && (t.r == '\n' || t.r == '\t'):
			plain = append(plain, byte(t.r))
			ends = append(ends, off)
		}
	}
	return plain, ends
}

// render draws b on a simple screen model and returns its text: trailing
// spaces and trailing empty lines are trimmed.
func render(b []byte) string {
	var rows [][]rune
	r, c := 0, 0
	line := func() []rune {
		for len(rows) <= r {
			rows = append(rows, nil)
		}
		return rows[r]
	}
	put := func(ch rune) {
		l := line()
		for len(l) <= c {
			l = append(l, ' ')
		}
		l[c] = ch
		rows[r] = l
		c++
	}
	arg := func(p []int, i, def int) int {
		if i < len(p) && p[i] > 0 {
			return p[i]
		}
		return def
	}
	for off := 0; off < len(b); {
		t := next(b[off:])
		if t.kind == tokIncomplete {
			break
		}
		off += t.n
		switch t.kind {
		case tokText:
			put(t.r)
		case tokControl:
			switch t.r {
			case '\n':
				r++
				line()
			case '\r':
				c = 0
			case '\b':
				if c > 0 {
					c--
				}
			case '\t':
				c = (c/8 + 1) * 8
			}
		case tokCSI:
			switch t.final {
			case 'K':
				l := line()
				mode := 0
				if len(t.params) > 0 {
					mode = t.params[0]
				}
				switch mode {
				case 0:
					if c < len(l) {
						rows[r] = l[:c]
					}
				case 1:
					for i := 0; i <= c && i < len(l); i++ {
						l[i] = ' '
					}
				case 2:
					rows[r] = nil
				}
			case 'J':
				mode := 0
				if len(t.params) > 0 {
					mode = t.params[0]
				}
				switch mode {
				case 0:
					if l := line(); c < len(l) {
						rows[r] = l[:c]
					}
					if r+1 < len(rows) {
						rows = rows[:r+1]
					}
				case 2, 3:
					rows = nil // content goes, the cursor stays
				}
			case 'A':
				r -= arg(t.params, 0, 1)
				if r < 0 {
					r = 0
				}
			case 'B':
				r += arg(t.params, 0, 1)
				line()
			case 'C':
				c += arg(t.params, 0, 1)
			case 'D':
				c -= arg(t.params, 0, 1)
				if c < 0 {
					c = 0
				}
			case 'G':
				c = arg(t.params, 0, 1) - 1
			case 'H', 'f':
				r, c = arg(t.params, 0, 1)-1, arg(t.params, 1, 1)-1
				line()
			}
		}
	}
	out := make([]string, len(rows))
	for i, l := range rows {
		out[i] = strings.TrimRight(string(l), " ")
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}
