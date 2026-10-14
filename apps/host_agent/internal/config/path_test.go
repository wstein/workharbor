// SPDX-License-Identifier: EUPL-1.2
package config_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
)

type pathValue struct {
	Kind       string          `json:"kind"`
	Value      json.RawMessage `json:"value"`
	Decimal    string          `json:"decimal"`
	Codepoints []int           `json:"codepoints"`
	Data       struct {
		Hex    string `json:"hex"`
		Length int    `json:"length"`
		SHA    string `json:"sha256"`
	} `json:"data"`
}

func pointString(points []int) string {
	var out []byte
	for _, p := range points {
		if p >= 0xd800 && p <= 0xdfff {
			// Preserve a source surrogate observation as invalid Go UTF-8, not U+FFFD.
			out = append(out, byte(0xe0|(p>>12)), byte(0x80|((p>>6)&63)), byte(0x80|(p&63)))
		} else {
			out = utf8.AppendRune(out, rune(p))
		}
	}
	return string(out)
}
func (v pathValue) materialize(t *testing.T) any {
	t.Helper()
	switch v.Kind {
	case "string":
		return pointString(v.Codepoints)
	case "null":
		return nil
	case "boolean":
		var b bool
		if e := json.Unmarshal(v.Value, &b); e != nil {
			t.Fatal(e)
		}
		return b
	case "integer":
		n, ok := new(big.Int).SetString(v.Decimal, 10)
		if !ok {
			t.Fatal("fixture integer")
		}
		return n
	case "float":
		var s string
		if e := json.Unmarshal(v.Value, &s); e != nil || s != "1.5" {
			t.Fatal("fixture float")
		}
		return 1.5
	case "bytes":
		b, e := hex.DecodeString(v.Data.Hex)
		if e != nil || len(b) != v.Data.Length || fmt.Sprintf("%x", sha256.Sum256(b)) != v.Data.SHA {
			t.Fatal("fixture bytes")
		}
		return b
	}
	t.Fatalf("unhandled path constructor %q", v.Kind)
	return nil
}
func pathCode(t *testing.T, e error) {
	t.Helper()
	var safe *config.Error
	if !errors.As(e, &safe) || safe.Code != "context_schema" || e.Error() != "context_schema" {
		t.Fatalf("want safe context_schema, got %v", e)
	}
}
func TestFrozenPathVectors(t *testing.T) {
	raw, e := os.ReadFile("../../../../contracts/migration/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "0070cf8057253dca19abe7eaaa55e9c9e17313ba53c6cd820014443e3708a04c" {
		t.Fatal("frozen vector hash changed")
	}
	var corpus struct {
		Cases []struct {
			ID        string    `json:"id"`
			Operation string    `json:"operation"`
			Input     pathValue `json:"input"`
			Dot       pathValue `json:"dot"`
			Expected  struct {
				Status     string `json:"status"`
				Code       string `json:"code"`
				Codepoints []int  `json:"codepoints"`
			} `json:"product_expected"`
		} `json:"cases"`
	}
	if e = json.Unmarshal(raw, &corpus); e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, c := range corpus.Cases {
		if c.Operation != "canonical-path" {
			continue
		}
		count++
		t.Run(c.ID, func(t *testing.T) {
			got, e := config.CanonicalPathWithDot(c.Input.materialize(t), c.Dot.materialize(t))
			if c.Expected.Status == "reject" {
				if c.Expected.Code != "context_schema" {
					t.Fatal("unexpected fixture code")
				}
				pathCode(t, e)
				return
			}
			if e != nil || got != pointString(c.Expected.Codepoints) {
				t.Fatalf("path mismatch: %q %v", got, e)
			}
		})
	}
	if count != 46 {
		t.Fatalf("ran %d path vectors, want 46", count)
	}
}
func TestLexicalPathNativeDomain(t *testing.T) {
	type namedString string
	type namedBool bool
	for _, v := range []any{nil, []byte("a"), 1, true, namedString("a"), (*string)(nil), string([]byte{0xff}), string([]byte{0xed, 0xa0, 0x80})} {
		_, e := config.CanonicalPath(v)
		pathCode(t, e)
	}
	for _, dot := range []any{nil, 0, 1, "", "true", namedBool(true), (*bool)(nil)} {
		for _, path := range []string{"a", "."} {
			_, e := config.CanonicalPathWithDot(path, dot)
			pathCode(t, e)
		}
	}
	for _, s := range []string{"x/.git/y", "a/./b", "a/../b", "a//b", "a/", "/a", "a\\b", "a\x00b", "a\nb", "a\rb", "a\tb", "a*b", "a?b", "a[b", "a]b", ""} {
		_, e := config.CanonicalPath(s)
		pathCode(t, e)
	}
	for _, s := range []string{"a", " C:x ", ".GIT/x", "%2e%2e/x", "~/a", "a\x1bb", "a\x01b", "�/𐀀", "e\u0301", "a\u2028b", strings.Repeat("a", 1048577)} {
		got, e := config.CanonicalPath(s)
		if e != nil || got != s {
			t.Fatalf("accepted spelling changed: %v", e)
		}
	}
	_, e := config.CanonicalPath(".")
	pathCode(t, e)
	got, e := config.CanonicalPathWithDot(".", true)
	if e != nil || got != "." {
		t.Fatal("dot=true failed")
	}
}
func FuzzLexicalPathIdentity(f *testing.F) {
	for _, s := range []string{"a", ".", "a/../b", "a\x1bb", "%2e%2e/x", string([]byte{0xff}), "�/𐀀"} {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, s string, dot bool) {
		got, e := config.CanonicalPathWithDot(s, dot)
		if e != nil {
			pathCode(t, e)
			return
		}
		if !utf8.ValidString(s) || got != s {
			t.Fatal("invalid UTF-8 accepted or path changed")
		}
		again, e := config.CanonicalPathWithDot(got, dot)
		if e != nil || again != got {
			t.Fatal("path not idempotent")
		}
	})
}
