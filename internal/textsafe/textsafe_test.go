package textsafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEscape(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain and valid UTF-8", "héllo ✓ 日本", "héllo ✓ 日本"},
		{"tab kept", "a\tb", "a\tb"},
		{"newline", "a\nb", `a\nb`},
		{"CR overwrite", "real\rfake", `real\rfake`},
		{"CRLF", "a\r\nb", `a\r\nb`},
		{"ESC CSI", "a\x1b[2Jb", `a\x1b[2Jb`},
		{"OSC title", "\x1b]0;pwned\x07", `\x1b]0;pwned\x07`},
		{"NUL", "a\x00b", `a\x00b`},
		{"DEL", "a\x7fb", `a\x7fb`},
		{"C1 CSI", "a\u009b31mb", `a\u009b31mb`},
		{"bidi override", "a\u202eb", `a\u202eb`},
		{"bidi isolate", "a\u2066b\u2069", `a\u2066b\u2069`},
		{"marks", "a\u200eb\u200f\u061c", `a\u200eb\u200f\u061c`},
		{"line and paragraph separator", "a\u2028b\u2029", `a\u2028b\u2029`},
		{"lone C1 byte", "a\x9bb", "a\ufffdb"},
		{"truncated sequence", "a\xe2\x82", "a\ufffd\ufffd"},
		{"real U+FFFD kept", "a\ufffdb", "a\ufffdb"},
	} {
		if got := Escape(tc.in); got != tc.want {
			t.Errorf("%s: Escape(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestIsControl(t *testing.T) {
	for r, want := range map[rune]bool{0: true, '\t': false, '\n': true, 0x1f: true, ' ': false, 0x7e: false, 0x7f: true, 0x80: true, 0x9f: true, 0xa0: false, 'é': false} {
		if got := IsControl(r); got != want {
			t.Errorf("IsControl(%U) = %v, want %v", r, got, want)
		}
	}
}

func TestEscapeJSONKeepsValidJSON(t *testing.T) {
	in := map[string]string{"s": "a\x1b[2J\u009b\x7f\u202e\u2028b\nc\té"}
	enc, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out := EscapeJSON(enc)
	if strings.ContainsAny(string(out), "\x7f\u009b\u202e\u2028\x1b") {
		t.Errorf("a control character is still raw: %q", out)
	}
	var back map[string]string
	if err := json.Unmarshal(out, &back); err != nil || back["s"] != in["s"] {
		t.Errorf("not the same document: %v %q", err, back)
	}
	if pretty := []byte("{\n  \"a\": \"\u009b\"\n}\n"); strings.Contains(string(EscapeJSON(pretty)), "\u009b") || !strings.HasSuffix(string(EscapeJSON(pretty)), "}\n") {
		t.Errorf("structural newlines must survive: %q", EscapeJSON(pretty))
	}
	if plain := []byte(`{"a":"é"}`); string(EscapeJSON(plain)) != string(plain) {
		t.Errorf("clean input changed")
	}
}

func TestEscapeJSONInvalidUTF8(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"lone C1 byte", "{\"a\":\"x\x9by\"}", "{\"a\":\"x\ufffdy\"}"},
		{"truncated sequence", "{\"a\":\"x\xe2\x82\"}", "{\"a\":\"x\ufffd\ufffd\"}"},
		{"real U+FFFD kept", "{\"a\":\"\ufffd\"}", "{\"a\":\"\ufffd\"}"},
	} {
		got := EscapeJSON([]byte(tc.in))
		if string(got) != tc.want || !utf8.Valid(got) {
			t.Errorf("%s: EscapeJSON(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// escapeJSONRef is the pre-#266 implementation, kept to check the refactor.
func escapeJSONRef(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		switch {
		case r == utf8.RuneError && n == 1:
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

func checkEscapeJSONSame(t *testing.T, in []byte) {
	t.Helper()
	got, want := EscapeJSON(append([]byte(nil), in...)), escapeJSONRef(in)
	if !bytes.Equal(got, want) {
		t.Errorf("EscapeJSON(%q) = %q, reference %q", in, got, want)
	}
	if utf8.Valid(in) && !utf8.Valid(got) {
		t.Errorf("EscapeJSON(%q) = %q is not valid UTF-8", in, got)
	}
}

func TestEscapeJSONFirstEscapePath(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"special after prefix", "abc\u009bdef", `abc\u009bdef`},
		{"invalid byte after prefix", "abc\x9bdef", "abc\ufffddef"},
		{"invalid byte at start", "\x9bdef", "\ufffddef"},
		{"invalid in prefix then special", "ab\xffc\u202ed", "ab\ufffdc\\u202ed"},
		{"special then invalid", "é\u2028\xc3", "é\\u2028\ufffd"},
		{"multibyte prefix", "日本\u007f日", `日本\u007f日`},
		{"no escape returns input", "日本abc", "日本abc"},
	} {
		got := EscapeJSON([]byte(tc.in))
		if string(got) != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		checkEscapeJSONSame(t, []byte(tc.in))
		if !utf8.Valid(got) {
			t.Errorf("%s: output %q not valid UTF-8", tc.name, got)
		}
	}
}

func FuzzEscapeJSON(f *testing.F) {
	for _, s := range []string{"", "abc", "abc\u009bd", "a\x9bb", "\xff", "é\u202e\xc3", "x\u007f"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		checkEscapeJSONSame(t, in)
		if out := EscapeJSON(append([]byte(nil), in...)); !utf8.Valid(out) && utf8.Valid(in) {
			t.Errorf("invalid output %q", out)
		}
	})
}
