// SPDX-License-Identifier: EUPL-1.2
// Package wirejson implements WorkHarbor's selected integer-only JSON profile.
// It is not RFC 8785/JCS. See contracts/migration for the frozen source profile
// and deliberate product restrictions. These primitives grant no authority.
package wirejson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxInputBytes          = 1 << 20
	MaxIntegerDigits       = 4300
	MaxContainerDepth      = 128
	CodeInvalidJSON        = "invalid-json"
	CodeInputLimitExceeded = "input-limit-exceeded"
)

// Error reports a fixed diagnostic without exposing input or decoder details.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }
func invalid() error           { return &Error{Code: CodeInvalidJSON} }

// Parse accepts exactly []byte, including a typed nil slice (which is invalid
// JSON). Other native types are invalid-json before any size inspection. Bytes
// above MaxInputBytes are input-limit-exceeded before UTF-8/syntax validation.
// The result consists of nil, bool, string, *big.Int, []any and map[string]any.
// Integers retain arbitrary precision within MaxIntegerDigits magnitude digits.
// Objects reject duplicate decoded keys; containers are bounded to depth 128.
func Parse(data any) (any, error) {
	raw, ok := data.([]byte)
	if !ok {
		return nil, invalid()
	}
	if len(raw) > MaxInputBytes {
		return nil, &Error{Code: CodeInputLimitExceeded}
	}
	// encoding/json substitutes malformed UTF-8 and unpaired surrogates. Validate
	// before tokenization so neither can become a valid replacement character.
	if !utf8.Valid(raw) || !json.Valid(raw) || !scalarEscapes(raw) {
		return nil, invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readValue(decoder, 0)
	if err != nil {
		return nil, invalid()
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, invalid()
	}
	return value, nil
}

// scalarEscapes runs only on syntactically valid JSON. Escaped backslashes must
// not turn a literal "\uD800" into a Unicode escape; paired escapes are consumed
// together. Literal UTF-8 has already been checked separately.
func scalarEscapes(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		i++
		for raw[i] != '"' {
			if raw[i] != '\\' {
				i++
				continue
			}
			i++
			if raw[i] != 'u' {
				i++
				continue
			}
			first := hexQuad(raw[i+1 : i+5])
			i += 5
			if first >= 0xd800 && first <= 0xdbff {
				if i+6 > len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
					return false
				}
				second := hexQuad(raw[i+2 : i+6])
				if second < 0xdc00 || second > 0xdfff {
					return false
				}
				i += 6
			} else if first >= 0xdc00 && first <= 0xdfff {
				return false
			}
		}
	}
	return true
}
func hexQuad(raw []byte) uint16 {
	var n uint16
	for _, b := range raw {
		n <<= 4
		switch {
		case b >= '0' && b <= '9':
			n += uint16(b - '0')
		case b >= 'a' && b <= 'f':
			n += uint16(b - 'a' + 10)
		default:
			n += uint16(b - 'A' + 10)
		}
	}
	return n
}
func readValue(decoder *json.Decoder, depth int) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch v := token.(type) {
	case nil, bool, string:
		return v, nil
	case json.Number:
		text := string(v)
		if strings.ContainsAny(text, ".eE") || magnitudeDigits(text) > MaxIntegerDigits {
			return nil, invalid()
		}
		n, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, invalid()
		}
		return n, nil
	case json.Delim:
		if depth >= MaxContainerDepth {
			return nil, invalid()
		}
		switch v {
		case '[':
			result := []any{}
			for decoder.More() {
				child, e := readValue(decoder, depth+1)
				if e != nil {
					return nil, e
				}
				result = append(result, child)
			}
			close, e := decoder.Token()
			if e != nil || close != json.Delim(']') {
				return nil, invalid()
			}
			return result, nil
		case '{':
			result := map[string]any{}
			for decoder.More() {
				key, e := decoder.Token()
				if e != nil {
					return nil, e
				}
				name, ok := key.(string)
				if !ok {
					return nil, invalid()
				}
				if _, exists := result[name]; exists {
					return nil, invalid()
				}
				child, e := readValue(decoder, depth+1)
				if e != nil {
					return nil, e
				}
				result[name] = child
			}
			close, e := decoder.Token()
			if e != nil || close != json.Delim('}') {
				return nil, invalid()
			}
			return result, nil
		}
	}
	return nil, invalid()
}
func magnitudeDigits(text string) int {
	if strings.HasPrefix(text, "-") {
		return len(text) - 1
	}
	return len(text)
}

// CanonicalBytes accepts exact native nil, bool, string, []any, map[string]any,
// *big.Int and built-in int/int8/int16/int32/int64/uint/uint8/uint16/uint32/uint64.
// Defined types, uintptr, arrays, byte slices, json.Number, floats, structs and
// pointers other than nonnil *big.Int are invalid-json; custom marshalers never
// execute. Nil exact slices/maps encode as []/{}. Strings/keys require valid
// UTF-8. No normalization, escaping of HTML/U+2028/U+2029 or trailing LF occurs.
// Depth/digit limits also apply here, and cycles fail within the depth bound.
// There is no canonical output byte-size cap. Callers must not mutate values
// concurrently while they are being read.
func CanonicalBytes(value any) ([]byte, error) {
	result, err := appendValue(nil, value, 0)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CanonicalDigest is lowercase SHA-256 of CanonicalBytes, with identical input
// validation. It inherits no parse-input byte-size cap.
func CanonicalDigest(value any) (string, error) {
	canonical, err := CanonicalBytes(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
func appendValue(out []byte, value any, depth int) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return append(out, "null"...), nil
	case bool:
		if v {
			return append(out, "true"...), nil
		}
		return append(out, "false"...), nil
	case string:
		return appendString(out, v)
	case []any:
		if depth >= MaxContainerDepth {
			return nil, invalid()
		}
		out = append(out, '[')
		for i, child := range v {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendValue(out, child, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	case map[string]any:
		if depth >= MaxContainerDepth {
			return nil, invalid()
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			if !utf8.ValidString(key) {
				return nil, invalid()
			}
			keys = append(keys, key)
		}
		// Valid UTF-8 byte ordering is Unicode scalar ordering, unlike UTF-16.
		sort.Strings(keys)
		out = append(out, '{')
		for i, key := range keys {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendString(out, key)
			if err != nil {
				return nil, err
			}
			out = append(out, ':')
			out, err = appendValue(out, v[key], depth+1)
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	default:
		text, ok := integerText(value)
		if !ok || magnitudeDigits(text) > MaxIntegerDigits {
			return nil, invalid()
		}
		return append(out, text...), nil
	}
}
func integerText(value any) (string, bool) {
	switch v := value.(type) {
	case *big.Int:
		if v != nil {
			return v.String(), true
		}
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	}
	return "", false
}
func appendString(out []byte, text string) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, invalid()
	}
	const digits = "0123456789abcdef"
	out = append(out, '"')
	for i := 0; i < len(text); i++ {
		b := text[i]
		switch b {
		case '"', '\\':
			out = append(out, '\\', b)
		case '\b':
			out = append(out, `\b`...)
		case '\f':
			out = append(out, `\f`...)
		case '\n':
			out = append(out, `\n`...)
		case '\r':
			out = append(out, `\r`...)
		case '\t':
			out = append(out, `\t`...)
		default:
			if b < 0x20 {
				out = append(out, '\\', 'u', '0', '0', digits[b>>4], digits[b&15])
			} else {
				out = append(out, b)
			}
		}
	}
	return append(out, '"'), nil
}
