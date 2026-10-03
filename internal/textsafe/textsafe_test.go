package textsafe

import (
	"encoding/json"
	"strings"
	"testing"
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
