// SPDX-License-Identifier: EUPL-1.2

package ptytest

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tokText       tokenKind = iota // one printable rune (or invalid byte)
	tokControl                     // C0 control byte
	tokCSI                         // ESC [ params final
	tokESC                         // ESC final (no intermediates)
	tokOther                       // OSC, other escapes, C1 controls: ignored
	tokIncomplete                  // truncated sequence at end of input
)

type token struct {
	kind    tokenKind
	n       int // bytes consumed
	r       rune
	params  []int
	private bool // CSI parameters began with '?'
	final   byte
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
		return token{kind: tokCSI, n: i + 1, params: parseParams(string(b[2:i])),
			private: i > 2 && b[2] == '?', final: b[i]}
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
	if i == 1 {
		return token{kind: tokESC, n: 2, final: b[1]}
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
// escape sequence removed except newline and tab, for each byte of that text
// the offset in b just after the token that produced it, and the number of
// bytes of b consumed. A truncated trailing sequence is left unconsumed.
func project(b []byte) (plain []byte, ends []uint32, consumed int) {
	off := 0
	for off < len(b) {
		if c := b[off]; c >= 0x20 && c < 0x7f { // fast path: printable ASCII
			plain = append(plain, c)
			off++
			ends = append(ends, uint32(off))
			continue
		}
		t := next(b[off:])
		if t.kind == tokIncomplete {
			break
		}
		start := off
		off += t.n
		switch {
		case t.kind == tokText && t.r == utf8.RuneError && t.n == 1:
			plain = append(plain, b[start])
			ends = append(ends, uint32(off))
		case t.kind == tokText:
			plain = append(plain, b[start:off]...)
			for i := start; i < off; i++ {
				ends = append(ends, uint32(off))
			}
		case t.kind == tokControl && (t.r == '\n' || t.r == '\t'):
			plain = append(plain, byte(t.r))
			ends = append(ends, uint32(off))
		}
	}
	return plain, ends, off
}

// excerpt quotes b for a failure message: all of it when short, otherwise the
// first and last 2 KiB and the total length.
func excerpt(b []byte) string {
	const keep = 2048
	if len(b) <= 2*keep {
		return fmt.Sprintf("%q", b)
	}
	return fmt.Sprintf("%q ... [%d bytes omitted of %d bytes] ... %q",
		b[:keep], len(b)-2*keep, len(b), b[len(b)-keep:])
}

type cell struct {
	r rune  // 0: blank, -1: right half of a wide character
	x int32 // 1-based index into screen.exts: combining marks of this cell
}

type altSave struct {
	rows   [][]cell
	r, c   int
	sr, sc int
	pend   bool
}

// screen is a small terminal model: a w by h grid (0 means unbounded) with
// history for scrolled-off lines. It is fed incrementally.
type screen struct {
	w, h   int
	hist   []string
	rows   [][]cell
	r, c   int
	sr, sc int
	alt    *altSave
	exts   []string
}

func newScreen(w, h int) *screen { return &screen{w: w, h: h} }

// render draws b on an unbounded screen and returns its text.
func render(b []byte) string {
	s := newScreen(0, 0)
	s.feed(b)
	return s.text()
}

func (s *screen) resize(w, h int) { s.w, s.h = w, h }

func (s *screen) row() []cell {
	for len(s.rows) <= s.r {
		s.rows = append(s.rows, nil)
	}
	return s.rows[s.r]
}

func (s *screen) rowText(l []cell) string {
	var sb strings.Builder
	for _, c := range l {
		switch {
		case c.r == 0:
			sb.WriteByte(' ')
		case c.r > 0:
			sb.WriteRune(c.r)
			if c.x > 0 {
				sb.WriteString(s.exts[c.x-1])
			}
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

func (s *screen) lineFeed() {
	s.r++
	if s.h > 0 && s.r >= s.h {
		if len(s.rows) > 0 {
			if s.alt == nil {
				s.hist = append(s.hist, s.rowText(s.rows[0]))
			}
			s.rows = s.rows[1:]
		} else if s.alt == nil {
			s.hist = append(s.hist, "")
		}
		s.r = s.h - 1
	}
	s.row()
}

func (s *screen) clampCol() {
	if s.w > 0 && s.c >= s.w {
		s.c = s.w - 1
	}
}

func width(r rune) int {
	if r < 0xad {
		return 1
	}
	switch {
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe6f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1faff, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

func (s *screen) put(r rune) {
	w := width(r)
	l := s.row()
	if w == 0 {
		if s.c > 0 && s.c-1 < len(l) {
			i := s.c - 1
			if l[i].r == -1 && i > 0 {
				i--
			}
			if l[i].x == 0 {
				s.exts = append(s.exts, "")
				l[i].x = int32(len(s.exts))
			}
			s.exts[l[i].x-1] += string(r)
		}
		return
	}
	if s.w > 0 && s.c+w > s.w {
		s.c = 0
		s.lineFeed()
		l = s.row()
	}
	for len(l) < s.c+w {
		l = append(l, cell{})
	}
	l[s.c] = cell{r: r}
	if w == 2 {
		l[s.c+1] = cell{r: -1}
	}
	s.rows[s.r] = l
	s.c += w
}

func arg(p []int, i, def int) int {
	if i < len(p) && p[i] > 0 {
		return p[i]
	}
	return def
}

func (s *screen) clampRow() {
	if s.r < 0 {
		s.r = 0
	}
	if s.h > 0 && s.r >= s.h {
		s.r = s.h - 1
	}
	s.row()
}

// feed draws the complete tokens at the start of b and returns the number of
// bytes consumed; a truncated trailing sequence is left for the next call.
func (s *screen) feed(b []byte) int {
	off := 0
	for off < len(b) {
		if c := b[off]; c >= 0x20 && c < 0x7f { // fast path: printable ASCII
			s.put(rune(c))
			off++
			continue
		}
		t := next(b[off:])
		if t.kind == tokIncomplete {
			break
		}
		off += t.n
		switch t.kind {
		case tokText:
			s.put(t.r)
		case tokControl:
			s.control(t.r)
		case tokESC:
			switch t.final {
			case '7':
				s.sr, s.sc = s.r, s.c
			case '8':
				s.r, s.c = s.sr, s.sc
				s.clampRow()
			}
		case tokCSI:
			s.csi(t)
		}
	}
	return off
}

func (s *screen) control(r rune) {
	switch r {
	case '\n':
		s.lineFeed()
	case '\r':
		s.c = 0
	case '\b':
		s.clampCol()
		if s.c > 0 {
			s.c--
		}
	case '\t':
		s.c = (s.c/8 + 1) * 8
		s.clampCol()
	}
}

func (s *screen) csi(t token) {
	p := t.params
	if t.private {
		if t.final == 'h' || t.final == 'l' {
			for _, v := range p {
				if v == 1049 || v == 1047 || v == 47 {
					s.setAlt(t.final == 'h')
				}
			}
		}
		return
	}
	switch t.final {
	case 'K':
		s.eraseLine(arg0(p))
	case 'J':
		switch arg0(p) {
		case 0:
			s.eraseLine(0)
			if s.r+1 < len(s.rows) {
				s.rows = s.rows[:s.r+1]
			}
		case 2, 3:
			s.rows = nil // content goes, the cursor stays
		}
	case 'A':
		s.clampCol()
		s.r -= arg(p, 0, 1)
		s.clampRow()
	case 'B':
		s.clampCol()
		s.r += arg(p, 0, 1)
		s.clampRow()
	case 'C':
		s.c += arg(p, 0, 1)
		s.clampCol()
	case 'D':
		s.clampCol()
		s.c -= arg(p, 0, 1)
		if s.c < 0 {
			s.c = 0
		}
	case 'E':
		s.r, s.c = s.r+arg(p, 0, 1), 0
		s.clampRow()
	case 'F':
		s.r, s.c = s.r-arg(p, 0, 1), 0
		s.clampRow()
	case 'G':
		s.c = arg(p, 0, 1) - 1
		s.clampCol()
	case 'd':
		s.r = arg(p, 0, 1) - 1
		s.clampRow()
	case 'H', 'f':
		s.r, s.c = arg(p, 0, 1)-1, arg(p, 1, 1)-1
		s.clampRow()
		s.clampCol()
	case 's':
		s.sr, s.sc = s.r, s.c
	case 'u':
		s.r, s.c = s.sr, s.sc
		s.clampRow()
	case 'L', 'M':
		s.row()
		n := arg(p, 0, 1)
		if t.final == 'L' {
			blank := make([][]cell, n)
			s.rows = append(s.rows[:s.r], append(blank, s.rows[s.r:]...)...)
			if s.h > 0 && len(s.rows) > s.h {
				s.rows = s.rows[:s.h]
			}
		} else {
			end := min(s.r+n, len(s.rows))
			s.rows = append(s.rows[:s.r], s.rows[end:]...)
		}
	case 'P', '@', 'X':
		s.editChars(t.final, arg(p, 0, 1))
	}
}

func arg0(p []int) int {
	if len(p) > 0 {
		return p[0]
	}
	return 0
}

func (s *screen) eraseLine(mode int) {
	s.clampCol()
	l := s.row()
	switch mode {
	case 0:
		if s.c < len(l) {
			s.rows[s.r] = l[:s.c]
		}
	case 1:
		for i := 0; i <= s.c && i < len(l); i++ {
			l[i] = cell{}
		}
	case 2:
		s.rows[s.r] = nil
	}
}

func (s *screen) editChars(op byte, n int) {
	s.clampCol()
	l := s.row()
	for len(l) < s.c {
		l = append(l, cell{})
	}
	switch op {
	case 'P':
		end := min(s.c+n, len(l))
		l = append(l[:s.c], l[end:]...)
	case '@':
		l = append(l[:s.c], append(make([]cell, n), l[s.c:]...)...)
		if s.w > 0 && len(l) > s.w {
			l = l[:s.w]
		}
	case 'X':
		for i := s.c; i < s.c+n && i < len(l); i++ {
			l[i] = cell{}
		}
	}
	s.rows[s.r] = l
}

func (s *screen) setAlt(on bool) {
	switch {
	case on && s.alt == nil:
		s.alt = &altSave{rows: s.rows, r: s.r, c: s.c, sr: s.sr, sc: s.sc}
		s.rows, s.r, s.c = nil, 0, 0
	case !on && s.alt != nil:
		a := s.alt
		s.rows, s.r, s.c, s.sr, s.sc = a.rows, a.r, a.c, a.sr, a.sc
		s.alt = nil
	}
}

func (s *screen) join(hist []string) string {
	lines := append([]string(nil), hist...)
	for _, l := range s.rows {
		lines = append(lines, s.rowText(l))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// text is the whole screen: scrolled-off lines then the visible rows, with
// trailing spaces and trailing empty lines trimmed.
func (s *screen) text() string { return s.join(s.hist) }

const tailLines = 256

// tail is the visible rows plus up to tailLines of history; it bounds the work
// of waiting for screen text on very long output.
func (s *screen) tail() string {
	h := s.hist
	if len(h) > tailLines {
		h = h[len(h)-tailLines:]
	}
	return s.join(h)
}
