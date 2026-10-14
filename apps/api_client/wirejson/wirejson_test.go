// SPDX-License-Identifier: EUPL-1.2
package wirejson_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
)

type byteVector struct {
	Encoding string `json:"encoding"`
	Hex      string `json:"hex"`
	Prefix   string `json:"prefix_hex"`
	Unit     string `json:"unit_hex"`
	Suffix   string `json:"suffix_hex"`
	Count    int    `json:"count"`
	Length   int    `json:"length"`
	SHA      string `json:"sha256"`
}

func (v byteVector) expand(t *testing.T) []byte {
	t.Helper()
	decode := func(s string) []byte {
		b, e := hex.DecodeString(s)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	var b []byte
	switch v.Encoding {
	case "hex":
		b = decode(v.Hex)
	case "repeat":
		b = append(decode(v.Prefix), bytes.Repeat(decode(v.Unit), v.Count)...)
		b = append(b, decode(v.Suffix)...)
	default:
		t.Fatalf("unknown bytes encoding %q", v.Encoding)
	}
	if len(b) != v.Length || fmt.Sprintf("%x", sha256.Sum256(b)) != v.SHA {
		t.Fatal("frozen byte materialization mismatch")
	}
	return b
}

type nativeVector struct {
	Kind       string          `json:"kind"`
	Value      json.RawMessage `json:"value"`
	Decimal    string          `json:"decimal"`
	Exponent   int             `json:"exponent"`
	Negative   bool            `json:"negative"`
	Codepoints []int           `json:"codepoints"`
	Codepoint  int             `json:"codepoint"`
	Count      int             `json:"count"`
	Data       byteVector      `json:"data"`
	Items      []nativeVector  `json:"items"`
	Entries    []struct {
		Key   nativeVector `json:"key"`
		Value nativeVector `json:"value"`
	} `json:"entries"`
	Depth int           `json:"depth"`
	Shape string        `json:"shape"`
	Leaf  *nativeVector `json:"leaf"`
}

// bytearray is a distinct native type, never silently converted to accepted []byte.
type bytearray []byte

type hostileMarshaler struct{}

func (hostileMarshaler) MarshalJSON() ([]byte, error) { panic("custom marshaler must never execute") }

func scalarString(points []int) string {
	var out []byte
	for _, p := range points {
		if p >= 0xd800 && p <= 0xdfff {
			// Surrogates have no scalar-string Go representation. Preserve their bytes
			// as invalid UTF-8 so real production validation rejects them, not U+FFFD.
			out = append(out, byte(0xe0|(p>>12)), byte(0x80|((p>>6)&63)), byte(0x80|(p&63)))
		} else {
			out = utf8.AppendRune(out, rune(p))
		}
	}
	return string(out)
}
func (v nativeVector) materialize(t *testing.T) any {
	t.Helper()
	switch v.Kind {
	case "null":
		return nil
	case "boolean":
		var b bool
		if e := json.Unmarshal(v.Value, &b); e != nil {
			t.Fatal(e)
		}
		return b
	case "integer":
		b, ok := new(big.Int).SetString(v.Decimal, 10)
		if !ok {
			t.Fatal("bad integer fixture")
		}
		return b
	case "power10":
		b := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(v.Exponent)), nil)
		if v.Negative {
			b.Neg(b)
		}
		return b
	case "string":
		return scalarString(v.Codepoints)
	case "repeat-string":
		return strings.Repeat(string(rune(v.Codepoint)), v.Count)
	case "bytes":
		return v.Data.expand(t)
	case "bytearray":
		return bytearray(v.Data.expand(t))
	case "float":
		var s string
		if e := json.Unmarshal(v.Value, &s); e != nil {
			t.Fatal(e)
		}
		switch s {
		case "1.5":
			return 1.5
		case "nan":
			return math.NaN()
		case "inf":
			return math.Inf(1)
		case "-inf":
			return math.Inf(-1)
		}
	case "list", "tuple":
		items := make([]any, len(v.Items))
		for i, x := range v.Items {
			items[i] = x.materialize(t)
		}
		if v.Kind == "list" {
			return items
		}
		array := reflect.New(reflect.ArrayOf(len(items), reflect.TypeFor[any]())).Elem()
		for i, x := range items {
			if x != nil {
				array.Index(i).Set(reflect.ValueOf(x))
			}
		}
		return array.Interface()
	case "object":
		object := map[string]any{}
		other := map[any]any{}
		stringsOnly := true
		for _, entry := range v.Entries {
			k, x := entry.Key.materialize(t), entry.Value.materialize(t)
			other[k] = x
			if s, ok := k.(string); ok {
				object[s] = x
			} else {
				stringsOnly = false
			}
		}
		if stringsOnly {
			return object
		}
		return other
	case "nested":
		x := v.Leaf.materialize(t)
		for i := 0; i < v.Depth; i++ {
			if v.Shape == "object" || (v.Shape == "alternating" && i%2 == 1) {
				x = map[string]any{"x": x}
			} else {
				x = []any{x}
			}
		}
		return x
	}
	t.Fatalf("unhandled native kind %q", v.Kind)
	return nil
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *wirejson.Error
	if !errors.As(err, &e) || e.Code != code || err.Error() != code {
		t.Fatalf("error=%v, want safe code %s", err, code)
	}
}
func TestFrozenJSONVectors(t *testing.T) {
	raw, e := os.ReadFile("../../../contracts/migration/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "0070cf8057253dca19abe7eaaa55e9c9e17313ba53c6cd820014443e3708a04c" {
		t.Fatal("frozen vector hash changed")
	}
	var corpus struct {
		Cases []struct {
			ID        string       `json:"id"`
			Operation string       `json:"operation"`
			Input     nativeVector `json:"input"`
			Expected  struct {
				Status    string     `json:"status"`
				Code      string     `json:"code"`
				Canonical byteVector `json:"canonical"`
				Digest    string     `json:"digest"`
			} `json:"product_expected"`
		} `json:"cases"`
	}
	if e = json.Unmarshal(raw, &corpus); e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, c := range corpus.Cases {
		if c.Operation == "canonical-path" {
			continue
		}
		count++
		t.Run(c.ID, func(t *testing.T) {
			v := c.Input.materialize(t)
			var output []byte
			var digest string
			var err error
			switch c.Operation {
			case "parse-json":
				v, err = wirejson.Parse(v)
			case "canonical-bytes":
				output, err = wirejson.CanonicalBytes(v)
			case "canonical-digest":
				digest, err = wirejson.CanonicalDigest(v)
			default:
				t.Fatal("unknown operation")
			}
			// Assert the selected operation before any follow-on operation can
			// mask a wrongly accepted input with its own rejection.
			if c.Expected.Status == "reject" {
				requireCode(t, err, c.Expected.Code)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Operation != "canonical-bytes" {
				output, err = wirejson.CanonicalBytes(v)
				if err != nil {
					t.Fatal(err)
				}
			}
			if c.Operation != "canonical-digest" {
				digest, err = wirejson.CanonicalDigest(v)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(output, c.Expected.Canonical.expand(t)) || digest != c.Expected.Digest {
				t.Fatalf("canonical bytes/digest mismatch for %s", c.ID)
			}
		})
	}
	if count != 113 {
		t.Fatalf("executed %d JSON vectors, want 113", count)
	}
}
func TestStrictJSONAdversarial(t *testing.T) {
	bad := []string{`{"x":[{"a":0,"\u0061":1}]}`, `{"\ud800":0}`, `["\udfff"]`, `"\ud800\u0041"`, `"\udc00\ud800"`, `"\ud800\\udc00"`, `true false`, `0 0`, `[01]`, `1e0`, `1.0`, "\xef\xbb\xbfnull", "\"\xed\xa0\x80\"", "\"\xf0\x80\x80\x80\"", `{"a":1,}`, `[]garbage`}
	for _, s := range bad {
		t.Run(fmt.Sprintf("%x", s), func(t *testing.T) { _, e := wirejson.Parse([]byte(s)); requireCode(t, e, "invalid-json") })
	}
	good := map[string]string{`"\ud800\udc00"`: "\"𐀀\"", `"\ufffd"`: "\"�\"", `{"�":1}`: `{"�":1}`, `"\\ud800"`: `"\\ud800"`, " \r\n\t [0,true] \n": "[0,true]", `{"\ud800\udc00":0}`: `{"𐀀":0}`}
	for s, want := range good {
		v, e := wirejson.Parse([]byte(s))
		if e != nil {
			t.Fatalf("%q: %v", s, e)
		}
		got, e := wirejson.CanonicalBytes(v)
		if e != nil || string(got) != want {
			t.Fatalf("%q: %q %v", s, got, e)
		}
	}
	_, e := wirejson.Parse(bytes.Repeat([]byte{0xff}, 1048577))
	requireCode(t, e, "input-limit-exceeded")
	_, e = wirejson.Parse(bytearray(bytes.Repeat([]byte{0xff}, 1048577)))
	requireCode(t, e, "invalid-json")
}
func TestCanonicalNativeDomain(t *testing.T) {
	type namedString string
	type namedInteger int
	type namedList []any
	type namedMap map[string]any
	var n *big.Int
	bad := []any{hostileMarshaler{}, uintptr(1), float32(1), float64(1), math.NaN(), math.Inf(-1), json.Number("1"), []byte("a"), [1]any{1}, map[int]any{1: 0}, map[any]any{"a": 0}, struct{ A int }{1}, namedString("a"), namedInteger(1), namedList{}, namedMap{}, n, (*string)(nil), string([]byte{0xff}), map[string]any{string([]byte{0xed, 0xa0, 0x80}): nil}}
	cycleList := make([]any, 1)
	cycleList[0] = cycleList
	cycleMap := map[string]any{}
	cycleMap["x"] = cycleMap
	bad = append(bad, cycleList, cycleMap)
	for i, v := range bad {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, e := wirejson.CanonicalBytes(v)
			requireCode(t, e, "invalid-json")
			_, e = wirejson.CanonicalDigest(v)
			requireCode(t, e, "invalid-json")
		})
	}
	for _, v := range []any{int(42), int8(42), int16(42), int32(42), int64(42), uint(42), uint8(42), uint16(42), uint32(42), uint64(42), big.NewInt(42)} {
		b, e := wirejson.CanonicalBytes(v)
		if e != nil || string(b) != "42" {
			t.Fatalf("%T: %q %v", v, b, e)
		}
	}
	for _, c := range []struct {
		v    any
		want string
	}{{nil, "null"}, {[]any(nil), "[]"}, {map[string]any(nil), "{}"}, {false, "false"}, {"\b\f\n\r\t\x00\x1f\"\\/<>\u2028\u2029", `"\b\f\n\r\t\u0000\u001f\"\\/<>` + "\u2028\u2029" + `"`}} {
		b, e := wirejson.CanonicalBytes(c.v)
		if e != nil || string(b) != c.want {
			t.Fatalf("%T: %q want %q: %v", c.v, b, c.want, e)
		}
	}
	large := strings.Repeat("x", 1048577)
	b, e := wirejson.CanonicalBytes(large)
	if e != nil || len(b) != 1048579 {
		t.Fatal("canonical output incorrectly bounded", e)
	}
}
func TestIntegerAndContainerLimits(t *testing.T) {
	for _, digits := range []int{4300, 4301} {
		for _, negative := range []bool{false, true} {
			s := strings.Repeat("9", digits)
			if negative {
				s = "-" + s
			}
			n, ok := new(big.Int).SetString(s, 10)
			if !ok {
				t.Fatal("test integer")
			}
			_, pe := wirejson.Parse([]byte(s))
			b, ce := wirejson.CanonicalBytes(n)
			d, de := wirejson.CanonicalDigest(n)
			if digits == 4301 {
				for _, e := range []error{pe, ce, de} {
					requireCode(t, e, "invalid-json")
				}
			} else if pe != nil || ce != nil || de != nil || string(b) != s || d != fmt.Sprintf("%x", sha256.Sum256([]byte(s))) {
				t.Fatal("4300 digit boundary failure")
			}
		}
	}
	for _, depth := range []int{128, 129} {
		var v any = nil
		for i := 0; i < depth; i++ {
			if i%2 == 0 {
				v = []any{v}
			} else {
				v = map[string]any{"x": v}
			}
		}
		b, e := wirejson.CanonicalBytes(v)
		_, de := wirejson.CanonicalDigest(v)
		if depth == 129 {
			requireCode(t, e, "invalid-json")
			requireCode(t, de, "invalid-json")
		} else {
			if e != nil || de != nil {
				t.Fatal(e, de)
			}
			if _, e = wirejson.Parse(b); e != nil {
				t.Fatal(e)
			}
		}
	}
	// Assert Parse's own result for every container shape, independent of
	// canonicalization, so a later encoder rejection cannot mask acceptance.
	for _, shape := range []string{"array", "object", "alternating"} {
		for _, depth := range []int{128, 129} {
			raw := "0"
			for i := 0; i < depth; i++ {
				if shape == "object" || (shape == "alternating" && i%2 == 1) {
					raw = `{"x":` + raw + `}`
				} else {
					raw = "[" + raw + "]"
				}
			}
			_, err := wirejson.Parse([]byte(raw))
			if depth == 129 {
				requireCode(t, err, "invalid-json")
			} else if err != nil {
				t.Fatalf("%s depth128: %v", shape, err)
			}
		}
	}

	v, e := wirejson.Parse([]byte("[true,1,-0]"))
	if e != nil {
		t.Fatal(e)
	}
	a := v.([]any)
	if _, ok := a[0].(bool); !ok {
		t.Fatal("boolean coerced")
	}
	if a[1].(*big.Int).Cmp(big.NewInt(1)) != 0 || a[2].(*big.Int).Sign() != 0 {
		t.Fatal("integer domain lost")
	}
}
func FuzzParseCanonicalRoundTrip(f *testing.F) {
	for _, s := range []string{`null`, `{"a":[true,-0,1]}`, `"\ud800"`, `{"a":0,"\u0061":1}`, `"\ud800\udc00"`, "\"�\u2028\""} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		v, e := wirejson.Parse(input)
		if e != nil {
			var safe *wirejson.Error
			if !errors.As(e, &safe) {
				t.Fatalf("unsafe error %v", e)
			}
			return
		}
		canonical, e := wirejson.CanonicalBytes(v)
		if e != nil {
			t.Fatal(e)
		}
		again, e := wirejson.Parse(canonical)
		if e != nil {
			t.Fatal(e)
		}
		stable, e := wirejson.CanonicalBytes(again)
		if e != nil || !bytes.Equal(canonical, stable) {
			t.Fatal("unstable canonicalization", e)
		}
		d, e := wirejson.CanonicalDigest(v)
		if e != nil || d != fmt.Sprintf("%x", sha256.Sum256(canonical)) {
			t.Fatal("digest disagreement", e)
		}
	})
}
