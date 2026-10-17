// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// Limits of the strict readers.
const (
	maxDepth = 128    // nesting levels of arrays and objects
	maxNodes = 100000 // values in one document
)

type jkind int

const (
	jNull jkind = iota
	jBool
	jNumber
	jString
	jArray
	jObject
)

// jvalue is a parsed JSON value. Numbers keep their exact literal in str so
// that 1, 1.0 and 1e0 stay distinguishable.
type jvalue struct {
	kind jkind
	str  string // string value or number literal
	b    bool
	arr  []jvalue
	obj  []jmember
}

type jmember struct {
	key string
	val jvalue
}

func (v jvalue) get(key string) (jvalue, bool) {
	for _, m := range v.obj {
		if m.key == key {
			return m.val, true
		}
	}
	return jvalue{}, false
}

// scanError is a failure of the scanner: a syntax error or a repeated key.
type scanError struct {
	duplicate bool
	msg       string
}

func (e *scanError) Error() string { return e.msg }

// parseStrict parses one JSON document (JSONC when jsonc is set: comments and
// trailing commas). The input must already be valid UTF-8 without BOM or NUL.
// The whole document is read so that a syntax error outranks a repeated key;
// the first repeated key is reported otherwise.
func parseStrict(data []byte, jsonc bool) (jvalue, *scanError) {
	p := &jparser{data: data, jsonc: jsonc}
	p.skip()
	v := p.value(1)
	if p.err == nil {
		p.skip()
		if p.err == nil && p.pos != len(p.data) {
			p.fail("unexpected content after the document")
		}
	}
	if p.err != nil {
		return jvalue{}, p.err
	}
	if p.dup != nil {
		return jvalue{}, p.dup
	}
	return v, nil
}

type jparser struct {
	data  []byte
	pos   int
	jsonc bool
	nodes int
	err   *scanError
	dup   *scanError
}

func (p *jparser) fail(format string, args ...any) {
	if p.err == nil {
		p.err = &scanError{msg: fmt.Sprintf(format, args...) + fmt.Sprintf(" (byte %d)", p.pos)}
	}
}

// skip passes whitespace and, for JSONC, comments.
func (p *jparser) skip() {
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case p.jsonc && c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/':
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.pos++
			}
		case p.jsonc && c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '*':
			end := -1
			for i := p.pos + 2; i+1 < len(p.data); i++ {
				if p.data[i] == '*' && p.data[i+1] == '/' {
					end = i + 2
					break
				}
			}
			if end < 0 {
				p.fail("unterminated comment")
				return
			}
			p.pos = end
		default:
			return
		}
	}
}

func (p *jparser) value(depth int) jvalue {
	if p.err != nil {
		return jvalue{}
	}
	if p.nodes++; p.nodes > maxNodes {
		p.fail("too many values")
		return jvalue{}
	}
	if p.pos >= len(p.data) {
		p.fail("unexpected end of input")
		return jvalue{}
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return jvalue{kind: jString, str: p.str()}
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case p.lit("true"):
		return jvalue{kind: jBool, b: true}
	case p.lit("false"):
		return jvalue{kind: jBool}
	case p.lit("null"):
		return jvalue{kind: jNull}
	}
	p.fail("unexpected character")
	return jvalue{}
}

func (p *jparser) lit(word string) bool {
	if len(p.data)-p.pos >= len(word) && string(p.data[p.pos:p.pos+len(word)]) == word {
		p.pos += len(word)
		return true
	}
	return false
}

func (p *jparser) number() jvalue {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
	}
	digits := func() int {
		n := 0
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
			n++
		}
		return n
	}
	switch {
	case p.pos < len(p.data) && p.data[p.pos] == '0':
		p.pos++
	case digits() == 0:
		p.fail("invalid number")
		return jvalue{}
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if digits() == 0 {
			p.fail("invalid number")
			return jvalue{}
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if digits() == 0 {
			p.fail("invalid number")
			return jvalue{}
		}
	}
	// A number followed by a name character, such as 01 or 1x, is not a number.
	if p.pos < len(p.data) {
		if c := p.data[p.pos]; c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '+' || c == '-' {
			p.fail("invalid number")
			return jvalue{}
		}
	}
	return jvalue{kind: jNumber, str: string(p.data[start:p.pos])}
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (p *jparser) hex4() rune {
	if p.pos+4 > len(p.data) {
		p.fail("invalid escape")
		return 0
	}
	var r rune
	for i := 0; i < 4; i++ {
		h := hexVal(p.data[p.pos+i])
		if h < 0 {
			p.fail("invalid escape")
			return 0
		}
		r = r<<4 | rune(h)
	}
	p.pos += 4
	return r
}

// str parses a string at the opening quote and returns its decoded value.
func (p *jparser) str() string {
	p.pos++ // opening quote
	var out []byte
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(out)
		case c < 0x20:
			p.fail("control character in string")
			return ""
		case c != '\\':
			out = append(out, c)
			p.pos++
		default:
			p.pos++
			if p.pos >= len(p.data) {
				p.fail("unterminated string")
				return ""
			}
			e := p.data[p.pos]
			p.pos++
			switch e {
			case '"', '\\', '/':
				out = append(out, e)
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				r := p.hex4()
				if p.err != nil {
					return ""
				}
				if utf16.IsSurrogate(r) {
					if r < 0xdc00 && p.pos+6 <= len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
						p.pos += 2
						lo := p.hex4()
						if p.err != nil {
							return ""
						}
						if r = utf16.DecodeRune(r, lo); r == utf8.RuneError {
							p.fail("unpaired surrogate")
							return ""
						}
					} else {
						p.fail("unpaired surrogate")
						return ""
					}
				}
				out = utf8.AppendRune(out, r)
			default:
				p.fail("invalid escape")
				return ""
			}
		}
	}
	p.fail("unterminated string")
	return ""
}

func (p *jparser) array(depth int) jvalue {
	if depth > maxDepth {
		p.fail("nesting deeper than %d levels", maxDepth)
		return jvalue{}
	}
	p.pos++
	v := jvalue{kind: jArray}
	p.skip()
	for p.err == nil {
		if p.pos < len(p.data) && p.data[p.pos] == ']' {
			p.pos++
			return v
		}
		v.arr = append(v.arr, p.value(depth+1))
		p.skip()
		if p.err != nil {
			break
		}
		switch {
		case p.pos < len(p.data) && p.data[p.pos] == ',':
			p.pos++
			p.skip()
			if !p.jsonc && p.pos < len(p.data) && p.data[p.pos] == ']' {
				p.fail("trailing comma")
			}
		case p.pos < len(p.data) && p.data[p.pos] == ']':
		default:
			p.fail("expected , or ]")
		}
	}
	return v
}

func (p *jparser) object(depth int) jvalue {
	if depth > maxDepth {
		p.fail("nesting deeper than %d levels", maxDepth)
		return jvalue{}
	}
	p.pos++
	v := jvalue{kind: jObject}
	seen := map[string]struct{}{}
	p.skip()
	for p.err == nil {
		if p.pos < len(p.data) && p.data[p.pos] == '}' {
			p.pos++
			return v
		}
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			p.fail("expected a string key")
			break
		}
		key := p.str()
		if p.err != nil {
			break
		}
		if _, dup := seen[key]; dup && p.dup == nil {
			p.dup = &scanError{duplicate: true, msg: "duplicate key " + quoteKey(key)}
		}
		seen[key] = struct{}{}
		p.skip()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			p.fail("expected :")
			break
		}
		p.pos++
		p.skip()
		val := p.value(depth + 1)
		v.obj = append(v.obj, jmember{key, val})
		p.skip()
		if p.err != nil {
			break
		}
		switch {
		case p.pos < len(p.data) && p.data[p.pos] == ',':
			p.pos++
			p.skip()
			if !p.jsonc && p.pos < len(p.data) && p.data[p.pos] == '}' {
				p.fail("trailing comma")
			}
		case p.pos < len(p.data) && p.data[p.pos] == '}':
		default:
			p.fail("expected , or }")
		}
	}
	return v
}

// quoteKey renders an untrusted key for a message: truncated, quoted with Go
// escapes so that no control character survives.
func quoteKey(k string) string { return fmt.Sprintf("%q", echo(k)) }
