// SPDX-License-Identifier: EUPL-1.2

package ptytest

import (
	"runtime"
	"strings"
	"testing"
)

func draw(w, h int, in string) string {
	in = strings.ReplaceAll(in, "\n", "\r\n") // as the tty line discipline does
	s := newScreen(w, h)
	if n := s.feed([]byte(in)); n != len(in) {
		return "INCOMPLETE"
	}
	return s.text()
}

func TestRenderSequences(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		in   string
		want string
	}{
		{"carriage return overwrites", 0, 0, "abc\rX", "Xbc"},
		{"backspace", 0, 0, "abc\b\bX", "aXc"},
		{"erase to end of line", 0, 0, "abcdef\r\x1b[3C\x1b[K", "abc"},
		{"erase to start of line", 0, 0, "abcdef\r\x1b[2C\x1b[1K", "   def"},
		{"erase whole line", 0, 0, "abc\x1b[2K\rZ", "Z"},
		{"cursor up", 0, 0, "one\ntwo\x1b[1A\rX", "Xne\ntwo"},
		{"cursor up clamps", 0, 0, "a\x1b[9AB", "aB"},
		{"cursor down and right", 0, 0, "a\x1b[2B\x1b[3CZ", "a\n\n    Z"},
		{"cursor left clamps", 0, 0, "ab\x1b[9DZ", "Zb"},
		{"absolute column", 0, 0, "abcdef\x1b[3GX", "abXdef"},
		{"absolute position", 0, 0, "a\nb\x1b[1;2HX", "aX\nb"},
		{"absolute row", 0, 0, "a\nb\nc\x1b[1dX", "aX\nb\nc"},
		{"next line and previous line", 0, 0, "ab\x1b[1Ecd\x1b[1F\x1b[1EX", "ab\nXd"},
		{"save and restore ESC 7 8", 0, 0, "ab\x1b7cd\x1b8X", "abXd"},
		{"save and restore CSI s u", 0, 0, "ab\x1b[scd\x1b[uX", "abXd"},
		{"tab", 0, 0, "a\tb", "a       b"},
		{"clear screen keeps cursor", 0, 0, "abc\x1b[2JX", "   X"},
		{"erase below", 0, 0, "a\nb\nc\x1b[2;1H\x1b[J", "a"},
		{"delete chars", 0, 0, "abcdef\r\x1b[2P", "cdef"},
		{"insert chars", 0, 0, "abc\r\x1b[2@", "  abc"},
		{"erase chars", 0, 0, "abcdef\r\x1b[2X", "  cdef"},
		{"insert and delete lines", 0, 0, "a\nb\nc\x1b[2;1H\x1b[1L", "a\n\nb\nc"},
		{"delete line", 0, 0, "a\nb\nc\x1b[2;1H\x1b[1M", "a\nc"},
		{"colors and OSC ignored", 0, 0, "\x1b[31mred\x1b[0m\x1b]0;title\x07!", "red!"},
		{"OSC with ST", 0, 0, "a\x1b]8;;http://x\x1b\\b", "ab"},
		{"charset escape ignored", 0, 0, "a\x1b(Bb", "ab"},
		{"wrap at width", 5, 0, "abcdefgh", "abcde\nfgh"},
		{"deferred wrap: exact width then newline", 5, 0, "abcde\r\nfg", "abcde\nfg"},
		{"scroll keeps history", 3, 2, "a\nb\nc\nd", "a\nb\nc\nd"},
		{"wide character takes two cells", 4, 0, "中中x", "中中\nx"},
		{"combining mark joins previous cell", 0, 0, "éx", "éx"},
		{"alt screen restored", 0, 0, "main\x1b[?1049halt\x1b[?1049l", "main"},
		{"alt screen content visible while active", 0, 0, "main\x1b[?1049halt", "alt"},
		{"truncated escape waits", 0, 0, "ok", "ok"},
	} {
		if got := draw(c.w, c.h, c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestFeedLeavesIncompleteTailUnconsumed(t *testing.T) {
	s := newScreen(0, 0)
	in := []byte("ab\x1b[3")
	if n := s.feed(in); n != 2 {
		t.Fatalf("consumed %d, want 2", n)
	}
	in = []byte("\xe2\x82")
	if n := newScreen(0, 0).feed(in); n != 0 {
		t.Fatalf("partial rune consumed %d", n)
	}
	if got := draw(0, 0, "\xe2\x82\xac"); got != "€" {
		t.Fatalf("complete rune: %q", got)
	}
	if got := draw(0, 0, "a\xffb"); got != "a�b" {
		t.Fatalf("invalid byte: %q", got)
	}
}

func TestProjectKeepsTextOnly(t *testing.T) {
	plain, ends, n := project([]byte("a\x1b[31mb\r\n\x1b]0;t\x07c\td\x00\x7f\u009be"), nil)
	if string(plain) != "ab\nc\tde" || n != len("a\x1b[31mb\r\n\x1b]0;t\x07c\td\x00\x7f\u009be") {
		t.Fatalf("plain %q consumed %d", plain, n)
	}
	if len(ends) != len(plain) || ends[0] != 1 || ends[1] != 7 {
		t.Fatalf("ends %v", ends)
	}
	// An escape sequence is part of the match end only up to the visible byte.
	plain, _, n = project([]byte("ab\x1b[3"), nil)
	if string(plain) != "ab" || n != 2 {
		t.Fatalf("truncated tail: %q %d", plain, n)
	}
	plain, _, n = project([]byte("a\xe2\x82"), nil)
	if string(plain) != "a" || n != 1 {
		t.Fatalf("partial rune: %q %d", plain, n)
	}
	plain, _, _ = project([]byte("\xe2\x82\xac\xff"), nil)
	if string(plain) != "€\xff" {
		t.Fatalf("invalid byte kept: %q", plain)
	}
}

func TestScreenTailBounded(t *testing.T) {
	s := newScreen(80, 24)
	s.feed([]byte(strings.Repeat("line\r\n", 5000) + "last"))
	tail := s.tail()
	if !strings.HasSuffix(tail, "last") || strings.Count(tail, "\n") > 300 {
		t.Fatalf("tail has %d lines", strings.Count(tail, "\n"))
	}
	if strings.Count(s.text(), "\n") != 5000 {
		t.Fatalf("text lost history: %d", strings.Count(s.text(), "\n"))
	}
}

func TestExcerptBounded(t *testing.T) {
	small := excerpt([]byte("hello"))
	if small != `"hello"` {
		t.Fatalf("small %s", small)
	}
	big := excerpt([]byte(strings.Repeat("x", 1<<20)))
	if len(big) > 5000 || !strings.Contains(big, "1048576 bytes") {
		t.Fatalf("excerpt %d bytes: %.80s", len(big), big)
	}
}

// countParams are the parameter spellings a hostile program can use.
var countParams = []string{"", "0", "1", "2147483648", "99999999999999999999",
	"1000000000", "65536", "1;99999999999999999999", "99999999999999999999;99999999999999999999",
	";", "-1", "1;2;3;4;5;6;7;8;9;10;11;12;13;14;15;16;17;18;19;20;1000000000"}

func TestCountParametersAreBoundedBeforeAllocating(t *testing.T) {
	finals := "@ABCDEFGHIJKLMPSTXZ`abdefghlmnqrsu"
	for _, size := range [][2]int{{80, 24}, {0, 0}, {1000, 1000}} {
		for _, f := range finals {
			for _, p := range countParams {
				for _, prefix := range []string{"", "?"} {
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					s := newScreen(size[0], size[1])
					in := "x\r\ny\x1b[" + prefix + p + string(f) + "z\x1b[" + prefix + p + string(f)
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Errorf("w=%d h=%d %q panicked: %v", size[0], size[1], in, r)
							}
						}()
						s.feed([]byte(in))
						_ = s.text()
					}()
					runtime.ReadMemStats(&after)
					if d := after.TotalAlloc - before.TotalAlloc; d > 64<<20 {
						t.Errorf("w=%d h=%d %q allocated %d bytes", size[0], size[1], in, d)
					}
					if s.h > 0 && len(s.rows) > s.h {
						t.Errorf("%q: %d rows on a %d row screen", in, len(s.rows), s.h)
					}
				}
			}
		}
	}
}

func TestInsertAndDeleteClampToTheScreen(t *testing.T) {
	if got := draw(80, 3, "a\r\nb\r\nc\x1b[1;1H\x1b[99999999999999999999L"); got != "" {
		t.Fatalf("insert beyond the screen clears it: %q", got)
	}
	if got := draw(5, 3, "abcde\x1b[1G\x1b[99999999999999999999@"); got != "" {
		t.Fatalf("insert beyond the width: %q", got)
	}
	if got := draw(80, 24, "abc\x1b[1G\x1b[99999999999999999999P"); got != "" {
		t.Fatalf("delete beyond the line: %q", got)
	}
	if got := draw(80, 24, "abc\x1b[1G\x1b[99999999999999999999X"); got != "" {
		t.Fatalf("erase beyond the line: %q", got)
	}
	if got := draw(80, 24, "ab\x1b[99999999999999999999Cz"); !strings.HasSuffix(got, "z") || len(got) != 80 {
		t.Fatalf("cursor right clamps to the last column: %q", got)
	}
}

func FuzzScreenFeed(f *testing.F) {
	for _, s := range []string{"", "a\x1b[1;1H", "\x1b[99999999999999999999L", "\x1b[?1049h\x1b[1000000000@",
		"\x1b]0;x", "\x1bP", "\x1b[", "\xe2\x82", "中\b中x\x1b7\x1b8", "a\x1b[2J\x1b[3A\x1b[9999D"} {
		f.Add([]byte(s), uint8(80), uint8(24))
	}
	f.Fuzz(func(t *testing.T, in []byte, w, h uint8) {
		s := newScreen(int(w)%100, int(h)%30)
		s.feed(in)
		_ = s.text()
		_ = s.tail()
		project(in, nil)
		if s.h > 0 && len(s.rows) > s.h {
			t.Fatalf("%d rows on a %d row screen", len(s.rows), s.h)
		}
	})
}

func TestNarrowCharacterOverWideCharacterBlanksTheOtherHalf(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"over the right half", "中\bx", " x"},
		{"over the left half", "中\b\bx", "x"},
		{"wide over two narrow", "ab\b\b中", "中"},
		{"wide over right half of wide", "中中\b\b\b中", " 中"},
		{"erase chars through the right half", "中\x1b[1G\x1b[1X", ""},
	} {
		if got := draw(0, 0, c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestUnterminatedSequencesAreCapped(t *testing.T) {
	for _, prefix := range []string{"\x1b]0;", "\x1bP", "\x1b_", "\x1b[1;", "\x1b[?"} {
		filler := "x"
		if strings.HasPrefix(prefix, "\x1b[") {
			filler = "1" // digits keep a CSI unterminated
		}
		long := strings.Repeat(filler, maxSeq+1000)
		s := newScreen(0, 0)
		if n := s.feed([]byte(prefix + long)); n != len(prefix+long) {
			t.Errorf("%q: long unterminated sequence not dropped (consumed %d)", prefix, n)
		}
		// The dropped sequence still ends where a real one would.
		end := "\x07"
		if strings.HasPrefix(prefix, "\x1b[") {
			end = "m"
		}
		s.feed([]byte(end + "END"))
		if !strings.HasSuffix(s.text(), "END") {
			t.Errorf("%q: text after the dropped sequence lost: %q", prefix, s.text())
		}
		if n := newScreen(0, 0).feed([]byte(prefix + filler + filler)); n != 0 {
			t.Errorf("%q: a short incomplete sequence must wait (consumed %d)", prefix, n)
		}
		if _, _, n := project([]byte(prefix+long), nil); n != len(prefix+long) {
			t.Errorf("%q: project kept a long unterminated sequence (%d)", prefix, n)
		}
	}
}

// feedChunks feeds chunks the way a Session does: unconsumed bytes are kept
// and retried with the next chunk.
func feedChunks(s *screen, chunks ...string) {
	var buf []byte
	for _, c := range chunks {
		buf = append(buf, c...)
		buf = buf[s.feed(buf):]
	}
}

func projectChunks(chunks ...string) string {
	var buf, plain []byte
	var st skipState
	for _, c := range chunks {
		buf = append(buf, c...)
		p, _, n := project(buf, &st)
		plain = append(plain, p...)
		buf = buf[n:]
	}
	return string(plain)
}

// TestChunkingNeverChangesTheResult: the screen and the visible text depend
// on the bytes only, never on where read boundaries fall, including around
// sequences longer than maxSeq that are dropped.
func TestChunkingNeverChangesTheResult(t *testing.T) {
	long := func(n int, f string) string { return strings.Repeat(f, n) }
	cases := map[string]string{
		"long OSC with BEL":      "A\x1b]0;" + long(maxSeq+500, "x") + "\x07B",
		"long OSC with ST":       "A\x1b]0;" + long(maxSeq+500, "x") + "\x1b\\B",
		"long OSC, ESC inside":   "A\x1b]0;" + long(maxSeq+10, "x") + "\x1bQ" + long(20, "y") + "\x07B",
		"long DCS with clear":    "A\x1bP" + long(maxSeq, "q") + "\x1b[2J" + long(30, "z") + "\x1b\\B",
		"long APC":               "A\x1b_" + long(maxSeq+1, "a") + "\x07B",
		"long CSI cursor":        "AB\x1b[" + long(maxSeq+100, "1") + "HC",
		"long CSI SGR":           "A\x1b[" + long(maxSeq+5, "1") + ";31mB",
		"long CSI final at-sign": "AB\x1b[" + long(maxSeq+5, "1") + "@C",
		"long CSI final tilde":   "AB\x1b[" + long(maxSeq+5, "1") + "~C",
		"long CSI bad final":     "A\x1b[" + long(maxSeq+5, "1") + "\x80B",
		"long CSI intermediate":  "A\x1b[" + long(maxSeq, "1") + " qB",
		"just under the cap":     "A\x1b]0;" + long(maxSeq-10, "x") + "\x07B",
		"unterminated OSC":       "A\x1b]0;" + long(maxSeq+5, "x") + "TAIL",
		"short sequences":        "A\x1b[31mB\x1b]0;t\x07C\x1b[2KD\r\nE",
	}
	for name, in := range cases {
		whole := newScreen(80, 24)
		n := whole.feed([]byte(in))
		_ = n
		want, wantPlain := whole.text(), projectChunks(in)
		var offsets []int
		for i := 0; i <= len(in); i++ {
			if i < 12 || i > len(in)-12 || (i > maxSeq-12 && i < maxSeq+16) || i%251 == 0 {
				offsets = append(offsets, i)
			}
		}
		for _, i := range offsets {
			s := newScreen(80, 24)
			feedChunks(s, in[:i], in[i:])
			if got := s.text(); got != want {
				t.Fatalf("%s split at %d: screen %q, whole %q", name, i, got, want)
			}
			if got := projectChunks(in[:i], in[i:]); got != wantPlain {
				t.Fatalf("%s split at %d: plain %.60q..., whole %.60q...", name, i, got, wantPlain)
			}
		}
		// Many tiny chunks as well.
		s := newScreen(80, 24)
		var parts []string
		for i := 0; i < len(in); i += 97 {
			parts = append(parts, in[i:min(i+97, len(in))])
		}
		feedChunks(s, parts...)
		if got := s.text(); got != want {
			t.Fatalf("%s in 97 byte chunks: %q, whole %q", name, got, want)
		}
	}
	// The visible results themselves, not only their agreement.
	for in, want := range map[string]string{
		"A\x1b]0;" + long(maxSeq+500, "x") + "\x07B":          "AB",
		"A\x1bP" + long(maxSeq, "q") + "\x1b[2J" + "z\x1b\\B": "AB",
		"AB\x1b[" + long(maxSeq+100, "1") + "HC":              "ABC",
		"A\x1b[" + long(maxSeq+5, "1") + ";31mB":              "AB",
	} {
		if got := draw(80, 24, in); got != want {
			t.Errorf("%.30q...: got %q want %q", in, got, want)
		}
	}
}

func TestEditsBreakWideCharactersAtTheirEdges(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"delete char on a right half", "\u4e2dab\x1b[2G\x1b[P", " ab"},
		{"delete the left half", "a\u4e2db\x1b[2G\x1b[P", "a b"},
		{"delete range ending on a left half", "ab\u4e2dc\x1b[1G\x1b[3P", " c"},
		{"delete range ending on a right half", "abc\u4e2d\x1b[1G\x1b[4P", ""},
		{"insert between the halves", "\u4e2dab\x1b[2G\x1b[@", "   ab"},
		{"erase to start through a left half", "\u4e2dab\x1b[1G\x1b[1K", "  ab"},
		{"erase to start ending on a right half", "\u4e2dab\x1b[2G\x1b[1K", "  ab"},
		{"erase to end from a right half", "\u4e2dab\x1b[2G\x1b[K", ""},
		{"overwrite over the wide pair", "\u4e2d\b\bxy", "xy"},
		{"orphaned right half renders as a blank", "\u4e2d\bx", " x"},
	} {
		if got := draw(0, 0, c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
