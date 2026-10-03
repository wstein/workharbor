package cli

import "testing"

func TestCleanStripsControlsAndBidi(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain text\tok", "plain text ok"},
		{"a\x1b[2Jb", "a?[2Jb"},
		{"a\u202eb", "a?b"},
		{"a\u202ab\u202c", "a?b?"},
		{"a\u2066b\u2069", "a?b?"},
		{"a\u200eb\u200f", "a?b?"},
		{"a\u061cb", "a?b"},
		{"a\u2028b\u2029", "a?b?"},
		{"a\u009b31m\x7f\x00b", "a?31m??b"},
		{"héllo ✓", "héllo ✓"},
	} {
		if got := clean(tc.in); got != tc.want {
			t.Errorf("clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
