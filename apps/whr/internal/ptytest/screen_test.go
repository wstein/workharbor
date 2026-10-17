// SPDX-License-Identifier: EUPL-1.2

package ptytest

import (
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
	plain, ends, n := project([]byte("a\x1b[31mb\r\n\x1b]0;t\x07c\td\x00\x7f\u009be"))
	if string(plain) != "ab\nc\tde" || n != len("a\x1b[31mb\r\n\x1b]0;t\x07c\td\x00\x7f\u009be") {
		t.Fatalf("plain %q consumed %d", plain, n)
	}
	if len(ends) != len(plain) || ends[0] != 1 || ends[1] != 7 {
		t.Fatalf("ends %v", ends)
	}
	// An escape sequence is part of the match end only up to the visible byte.
	plain, _, n = project([]byte("ab\x1b[3"))
	if string(plain) != "ab" || n != 2 {
		t.Fatalf("truncated tail: %q %d", plain, n)
	}
	plain, _, n = project([]byte("a\xe2\x82"))
	if string(plain) != "a" || n != 1 {
		t.Fatalf("partial rune: %q %d", plain, n)
	}
	plain, _, _ = project([]byte("\xe2\x82\xac\xff"))
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
