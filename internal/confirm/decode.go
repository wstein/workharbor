package confirm

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Decode parses data strictly and returns a validated Record. It rejects
// invalid UTF-8, duplicate keys, null, floats, unknown top-level fields
// (outside ext), trailing bytes and any input whose canonical re-encoding is
// not byte-identical to data.
func Decode(data []byte) (Record, error) {
	p := &parser{b: data}
	v, err := p.value(0)
	if err != nil {
		return Record{}, err
	}
	if p.i != len(p.b) {
		return Record{}, fmt.Errorf("%w: trailing bytes", ErrSyntax)
	}
	top, ok := v.(map[string]any)
	if !ok {
		return Record{}, fmt.Errorf("%w: not an object", ErrSyntax)
	}
	r, err := fromTree(top)
	if err != nil {
		return Record{}, err
	}
	// fromTree already rejects an unknown v, so no version check is needed here.
	canon, err := Encode(r)
	if err != nil {
		return Record{}, err
	}
	if !bytes.Equal(canon, data) {
		return Record{}, ErrNotCanonical
	}
	if err := r.Validate(); err != nil {
		return Record{}, err
	}
	return r, nil
}

var topFields = map[string]bool{
	"v": true, "schema": true, "action": true, "subject": true, "question": true,
	"answer": true, "at": true, "channel": true, "by": true, "assurance": true,
	"evidence": true, "ext": true,
}

func fromTree(top map[string]any) (r Record, err error) {
	for k := range top {
		if !topFields[k] {
			return r, fmt.Errorf("%w: %q", ErrUnknownField, k)
		}
	}
	for _, k := range []string{"v", "schema", "action", "subject", "answer", "at", "channel", "by", "assurance"} {
		if _, ok := top[k]; !ok {
			return r, fmt.Errorf("%w: %s", ErrMissingRequired, k)
		}
	}
	v, ok := top["v"].(int64)
	if !ok {
		return r, fmt.Errorf("%w: v must be an integer", ErrSyntax)
	}
	if v != Version {
		// Compare as int64 so an out-of-range v is never truncated.
		return r, fmt.Errorf("%w: %d", ErrUnknownVersion, v)
	}
	r.V = Version
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"schema", &r.Schema},
		{"action", &r.Action},
		{"at", &r.At},
		{"channel", &r.Channel},
		{"by", &r.By},
		{"assurance", &r.Assurance},
		{"question", &r.Question},
	} {
		if x, present := top[f.key]; present {
			s, ok := x.(string)
			if !ok {
				return r, fmt.Errorf("%w: %s must be a string", ErrSyntax, f.key)
			}
			*f.dst = s
		}
	}
	if r.Subject, err = subjectFrom(top["subject"]); err != nil {
		return r, err
	}
	am, ok := top["answer"].(map[string]any)
	if !ok {
		return r, fmt.Errorf("%w: answer must be an object", ErrBadAnswer)
	}
	if err = onlyKeys(am, ErrBadAnswer, "mode", "value"); err != nil {
		return r, err
	}
	if r.Answer.Mode, err = str(am, "mode", ErrBadAnswer); err != nil {
		return r, err
	}
	if r.Answer.Value, err = str(am, "value", ErrBadAnswer); err != nil {
		return r, err
	}
	if x, present := top["evidence"]; present {
		list, ok := x.([]any)
		if !ok || len(list) == 0 {
			return r, fmt.Errorf("%w: evidence must be a non-empty array when present", ErrBadEvidence)
		}
		for _, item := range list {
			em, ok := item.(map[string]any)
			if !ok {
				return r, fmt.Errorf("%w: item must be an object", ErrBadEvidence)
			}
			if err = onlyKeys(em, ErrBadEvidence, "kind", "name", "object", "ref", "result", "value"); err != nil {
				return r, err
			}
			var e Evidence
			if e.Kind, err = str(em, "kind", ErrBadEvidence); err != nil {
				return r, err
			}
			for _, f := range []struct {
				key string
				dst *string
			}{
				{"name", &e.Name},
				{"object", &e.Object},
				{"ref", &e.Ref},
				{"result", &e.Result},
				{"value", &e.Value},
			} {
				if *f.dst, err = str(em, f.key, ErrBadEvidence); err != nil {
					return r, err
				}
			}
			r.Evidence = append(r.Evidence, e)
		}
	}
	if x, present := top["ext"]; present {
		em, ok := x.(map[string]any)
		if !ok || len(em) == 0 {
			return r, fmt.Errorf("%w: ext must be a non-empty object when present", ErrBadExt)
		}
		r.Ext = em
	}
	return r, nil
}

func subjectFrom(x any) (s Subject, err error) {
	m, ok := x.(map[string]any)
	if !ok {
		return s, fmt.Errorf("%w: not an object", ErrBadSubject)
	}
	if err = onlyKeys(m, ErrBadSubject, "commit", "branch", "issue", "decision", "ref"); err != nil {
		return s, err
	}
	for _, f := range []struct {
		key string
		dst *string
	}{{"commit", &s.Commit}, {"branch", &s.Branch}, {"issue", &s.Issue}, {"decision", &s.Decision}, {"ref", &s.Ref}} {
		if *f.dst, err = str(m, f.key, ErrBadSubject); err != nil {
			return s, err
		}
	}
	return s, nil
}

func onlyKeys(m map[string]any, base error, allowed ...string) error {
outer:
	for k := range m {
		for _, a := range allowed {
			if k == a {
				continue outer
			}
		}
		return fmt.Errorf("%w: unknown key %q", base, k)
	}
	return nil
}

func str(m map[string]any, key string, base error) (string, error) {
	x, present := m[key]
	if !present {
		return "", nil
	}
	s, ok := x.(string)
	if !ok {
		return "", fmt.Errorf("%w: %s must be a string", base, key)
	}
	return s, nil
}

// parser is a strict JSON reader that yields map[string]any, []any, string,
// int64 and bool, and rejects what the canonical form forbids.
type parser struct {
	b []byte
	i int
}

const maxDepth = 16

func (p *parser) fail(msg string) error {
	return fmt.Errorf("%w: %s at byte %d", ErrSyntax, msg, p.i)
}

func (p *parser) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, p.fail("nesting too deep")
	}
	if p.i >= len(p.b) {
		return nil, p.fail("unexpected end")
	}
	switch c := p.b[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return p.str()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case bytes.HasPrefix(p.b[p.i:], []byte("true")):
		p.i += 4
		return true, nil
	case bytes.HasPrefix(p.b[p.i:], []byte("false")):
		p.i += 5
		return false, nil
	case bytes.HasPrefix(p.b[p.i:], []byte("null")):
		return nil, p.fail("null not allowed")
	}
	return nil, p.fail("unexpected character")
}

func (p *parser) object(depth int) (any, error) {
	p.i++
	m := map[string]any{}
	if p.i < len(p.b) && p.b[p.i] == '}' {
		p.i++
		return m, nil
	}
	for {
		if p.i >= len(p.b) || p.b[p.i] != '"' {
			return nil, p.fail("expected key")
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		if _, dup := m[k]; dup {
			return nil, p.fail("duplicate key " + strconv.Quote(k))
		}
		if p.i >= len(p.b) || p.b[p.i] != ':' {
			return nil, p.fail("expected colon")
		}
		p.i++
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		m[k] = v
		if p.i >= len(p.b) {
			return nil, p.fail("unexpected end")
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return m, nil
		default:
			return nil, p.fail("expected comma or brace")
		}
	}
}

func (p *parser) array(depth int) (any, error) {
	p.i++
	list := []any{}
	if p.i < len(p.b) && p.b[p.i] == ']' {
		p.i++
		return list, nil
	}
	for {
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		if p.i >= len(p.b) {
			return nil, p.fail("unexpected end")
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return list, nil
		default:
			return nil, p.fail("expected comma or bracket")
		}
	}
}

func (p *parser) number() (any, error) {
	start := p.i
	if p.b[p.i] == '-' {
		p.i++
	}
	ds := p.i
	for p.i < len(p.b) && p.b[p.i] >= '0' && p.b[p.i] <= '9' {
		p.i++
	}
	if p.i == ds {
		return nil, p.fail("bad number")
	}
	if p.i < len(p.b) && (p.b[p.i] == '.' || p.b[p.i] == 'e' || p.b[p.i] == 'E') {
		return nil, p.fail("floats not allowed")
	}
	text := string(p.b[start:p.i])
	if (p.i-ds > 1 && p.b[ds] == '0') || text == "-0" {
		return nil, p.fail("non-canonical integer")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, p.fail("integer out of range")
	}
	return n, nil
}

func (p *parser) str() (string, error) {
	p.i++ // opening quote
	var out []byte
	for p.i < len(p.b) {
		c := p.b[p.i]
		switch {
		case c == '"':
			p.i++
			return string(out), nil
		case c < 0x20:
			return "", p.fail("control character in string")
		case c == '\\':
			p.i++
			if p.i >= len(p.b) {
				return "", p.fail("bad escape")
			}
			e := p.b[p.i]
			p.i++
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
				r, err := p.hex4()
				if err != nil {
					return "", err
				}
				if r >= 0xd800 && r < 0xdc00 {
					if !bytes.HasPrefix(p.b[p.i:], []byte(`\u`)) {
						return "", p.fail("lone surrogate")
					}
					p.i += 2
					lo, err := p.hex4()
					if err != nil {
						return "", err
					}
					if lo < 0xdc00 || lo > 0xdfff {
						return "", p.fail("lone surrogate")
					}
					r = 0x10000 + (r-0xd800)<<10 + (lo - 0xdc00)
				} else if r >= 0xdc00 && r <= 0xdfff {
					return "", p.fail("lone surrogate")
				}
				out = utf8.AppendRune(out, r)
			default:
				return "", p.fail("bad escape")
			}
		default:
			r, size := utf8.DecodeRune(p.b[p.i:])
			if r == utf8.RuneError && size == 1 {
				return "", p.fail("invalid UTF-8")
			}
			out = append(out, p.b[p.i:p.i+size]...)
			p.i += size
		}
	}
	return "", p.fail("unterminated string")
}

func (p *parser) hex4() (rune, error) {
	if p.i+4 > len(p.b) {
		return 0, p.fail("bad unicode escape")
	}
	n, err := strconv.ParseUint(string(p.b[p.i:p.i+4]), 16, 16)
	if err != nil {
		return 0, p.fail("bad unicode escape")
	}
	p.i += 4
	return rune(n), nil //nolint:gosec // 16 bits parsed
}
