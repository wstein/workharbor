// SPDX-License-Identifier: EUPL-1.2

package terminal_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

// corpus holds one hostile sample per FR-022 control class.
var corpus = []struct {
	class string
	text  string
	human string
}{
	{"C0 NUL", "a\x00b", "a<U+0000>b"},
	{"C0 ESC", "a\x1b[31mb", "a<U+001B>[31mb"},
	{"C0 LF", "a\nb", "a<U+000A>b"},
	{"C0 CR", "a\rb", "a<U+000D>b"},
	{"C0 BEL", "a\ab", "a<U+0007>b"},
	{"C0 US", "a\x1fb", "a<U+001F>b"},
	{"DEL", "a\x7fb", "a<U+007F>b"},
	{"C1 CSI", "a\u009bb", "a<U+009B>b"},
	{"C1 OSC", "a\u009db", "a<U+009D>b"},
	{"C1 NEL", "a\u0085b", "a<U+0085>b"},
	{"C1 last", "a\u009fb", "a<U+009F>b"},
	{"bidi LRM", "a\u200eb", "a<U+200E>b"},
	{"bidi RLM", "a\u200fb", "a<U+200F>b"},
	{"bidi ALM", "a\u061cb", "a<U+061C>b"},
	{"bidi LRE", "a\u202ab", "a<U+202A>b"},
	{"bidi RLO", "a\u202eb", "a<U+202E>b"},
	{"bidi LRI", "a\u2066b", "a<U+2066>b"},
	{"bidi PDI", "a\u2069b", "a<U+2069>b"},
	{"line separator", "a\u2028b", "a<U+2028>b"},
	{"paragraph separator", "a\u2029b", "a<U+2029>b"},
	{"Cf zero width space", "a\u200bb", "a<U+200B>b"},
	{"Cf BOM", "a\ufeffb", "a<U+FEFF>b"},
	{"Cf soft hyphen", "a\u00adb", "a<U+00AD>b"},
	{"Cf tag character", "a\U000e0041b", "a<U+E0041>b"},
	{"invalid UTF-8", "a\xffb", "a<0xFF>b"},
	{"truncated UTF-8", "a\xe2\x82", "a<0xE2><0x82>"},
}

func TestEscapeFR022Corpus(t *testing.T) {
	for _, tc := range corpus {
		t.Run("human "+tc.class, func(t *testing.T) {
			got := terminal.Escape(tc.text)
			if got != tc.human {
				t.Fatalf("Escape(%q) = %q, want %q", tc.text, got, tc.human)
			}
			assertInert(t, got)
		})
		t.Run("json "+tc.class, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"k": strings.ToValidUTF8(tc.text, "\ufffd")})
			if err != nil {
				t.Fatal(err)
			}
			got := terminal.EscapeJSON(raw)
			assertInert(t, strings.ReplaceAll(string(got), "\t", ""))
			if !json.Valid(got) {
				t.Fatalf("EscapeJSON produced invalid JSON: %q", got)
			}
			var back map[string]string
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatal(err)
			}
			if want := strings.ToValidUTF8(tc.text, "\ufffd"); back["k"] != want {
				t.Fatalf("round trip changed the value: %q != %q", back["k"], want)
			}
		})
	}
}

func TestEscapeKeepsSafeText(t *testing.T) {
	for _, s := range []string{"", "plain", "tab\tkept", "\u00fcn\u00efc\u00f6d\u00e9 \u2713 \u65e5\u672c\u8a9e \u2014 emoji \U0001f642", "~ and }"} {
		if got := terminal.Escape(s); got != s {
			t.Errorf("Escape(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestEscapeExhaustiveRuneRange(t *testing.T) {
	for _, r := range scalarValues() {
		got := terminal.Escape(string(r))
		if terminal.IsUnsafe(r) {
			if !strings.HasPrefix(got, "<U+") {
				t.Fatalf("U+%04X is unsafe but escaped to %q", r, got)
			}
			continue
		}
		if got != string(r) {
			t.Fatalf("U+%04X is safe but escaped to %q", r, got)
		}
		if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && r != 0x09) {
			t.Fatalf("U+%04X is a control or format rune reported safe", r)
		}
	}
}

func TestEscapeJSONExhaustiveRuneRange(t *testing.T) {
	for _, r := range scalarValues() {
		raw, _ := json.Marshal(string(r))
		got := terminal.EscapeJSON(raw)
		if terminal.IsUnsafe(r) && strings.ContainsRune(string(got), r) {
			t.Fatalf("U+%04X survives raw in JSON output %q", r, got)
		}
		var back string
		if err := json.Unmarshal(got, &back); err != nil || back != string(r) {
			t.Fatalf("U+%04X does not round trip: %q, %v", r, back, err)
		}
	}
}

func TestEscapeIsBoundedGrowth(t *testing.T) {
	in := strings.Repeat("\x1b", 1<<16)
	if got := terminal.Escape(in); len(got) != len(in)*len("<U+001B>") {
		t.Fatalf("unexpected output length %d", len(got))
	}
}

// assertInert fails when s still contains a rune of any FR-022 class.
func assertInert(t *testing.T, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Fatalf("output is not valid UTF-8: %q", s)
	}
	for _, r := range s {
		if terminal.IsUnsafe(r) {
			t.Fatalf("output %q contains unsafe rune U+%04X", s, r)
		}
	}
}

// scalarValues lists every scalar value of the planes that hold assigned
// controls or format characters: planes 0 to 2 and the tag block of plane 14.
func scalarValues() []rune {
	var out []rune
	for r := rune(0); r < 0x30000; r++ {
		if r < 0xd800 || r > 0xdfff {
			out = append(out, r)
		}
	}
	for r := rune(0xe0000); r < 0xe1000; r++ {
		out = append(out, r)
	}
	out = append(out, unicode.MaxRune)
	return out
}
