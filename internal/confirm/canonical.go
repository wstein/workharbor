package confirm

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"
)

// Encode returns the canonical bytes of r: UTF-8 JSON, bytewise-sorted keys,
// no whitespace, \uXXXX escapes for non-ASCII, integers only, no null, empty
// optionals omitted. It does not validate r. It rejects values beyond the
// decoder's nesting limit, including cyclic extension maps and slices.
func Encode(r Record) ([]byte, error) {
	m := map[string]any{
		"v":         int64(r.V),
		"schema":    r.Schema,
		"action":    r.Action,
		"subject":   subjectMap(r.Subject),
		"answer":    answerMap(r.Answer),
		"at":        r.At,
		"channel":   r.Channel,
		"by":        r.By,
		"assurance": r.Assurance,
	}
	if r.Question != "" {
		m["question"] = r.Question
	}
	if len(r.Evidence) > 0 {
		ev := make([]any, len(r.Evidence))
		for i, e := range r.Evidence {
			ev[i] = evidenceMap(e)
		}
		m["evidence"] = ev
	}
	if len(r.Ext) > 0 {
		m["ext"] = r.Ext
	}
	var b bytes.Buffer
	if err := writeValue(&b, m, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func subjectMap(s Subject) map[string]any {
	m := map[string]any{}
	if s.Commit != "" {
		m["commit"] = s.Commit
	}
	if s.Branch != "" {
		m["branch"] = s.Branch
	}
	if s.Issue != "" {
		m["issue"] = s.Issue
	}
	if s.Decision != "" {
		m["decision"] = s.Decision
	}
	if s.Ref != "" {
		m["ref"] = s.Ref
	}
	return m
}

func answerMap(a Answer) map[string]any {
	m := map[string]any{"mode": a.Mode}
	if a.Value != "" {
		m["value"] = a.Value
	}
	return m
}

func evidenceMap(e Evidence) map[string]any {
	m := map[string]any{"kind": e.Kind}
	for k, v := range map[string]string{"name": e.Name, "object": e.Object, "ref": e.Ref, "result": e.Result, "value": e.Value} {
		if v != "" {
			m[k] = v
		}
	}
	return m
}

func encodeEvidence(e Evidence) []byte {
	var b bytes.Buffer
	_ = writeValue(&b, evidenceMap(e), 0)
	return b.Bytes()
}

func writeValue(b *bytes.Buffer, v any, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("%w: nesting too deep", ErrSyntax)
	}
	switch x := v.(type) {
	case string:
		return writeString(b, x)
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case int:
		b.WriteString(strconv.Itoa(x))
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeValue(b, e, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // Go string order is bytewise
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := writeValue(b, x[k], depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("%w: unsupported value %T (no null, no floats)", ErrSyntax, v)
	}
	return nil
}

const hexDigits = "0123456789abcdef"

func writeString(b *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: invalid UTF-8", ErrSyntax)
	}
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r > 0x7e:
			if r > 0xffff {
				r -= 0x10000
				writeU(b, 0xd800+(r>>10))
				writeU(b, 0xdc00+(r&0x3ff))
			} else {
				writeU(b, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return nil
}

func writeU(b *bytes.Buffer, r rune) {
	b.WriteString(`\u`)
	for s := 12; s >= 0; s -= 4 {
		b.WriteByte(hexDigits[(r>>uint(s))&0xf])
	}
}
