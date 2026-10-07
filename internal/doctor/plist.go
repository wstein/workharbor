package doctor

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
)

// Limits for a property list that comes from a command: its text is untrusted.
const (
	plistMaxBytes = 4 << 20
	plistMaxNodes = 100000
	plistMaxDepth = 24
)

// parsePlist reads an XML property list (what `diskutil ... -plist` prints) into
// map[string]any, []any, string, int64, float64 and bool. Anything else, a
// document that is too big or too deep, or text that is not a plist is an
// error; nothing here panics on hostile input.
func parsePlist(b []byte) (any, error) {
	if len(b) > plistMaxBytes {
		return nil, errors.New("the property list is too large")
	}
	p := &plistReader{d: xml.NewDecoder(bytes.NewReader(b))}
	for {
		se, ok, err := p.start()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("no property list")
		}
		if se.Name.Local == "plist" {
			break
		}
	}
	se, ok, err := p.start()
	if err != nil || !ok {
		return nil, errors.New("the property list is empty")
	}
	return p.value(se, 0)
}

type plistReader struct {
	d     *xml.Decoder
	nodes int
}

// start returns the next start element; ok is false at an end element or the
// end of the input.
func (p *plistReader) start() (se xml.StartElement, ok bool, err error) {
	for {
		tok, err := p.d.Token()
		if errors.Is(err, io.EOF) {
			return se, false, nil
		}
		if err != nil {
			return se, false, errors.New("the property list is not valid XML")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return t, true, nil
		case xml.EndElement:
			return se, false, nil
		}
	}
}

func (p *plistReader) value(se xml.StartElement, depth int) (any, error) {
	if depth > plistMaxDepth {
		return nil, errors.New("the property list is nested too deeply")
	}
	if p.nodes++; p.nodes > plistMaxNodes {
		return nil, errors.New("the property list has too many entries")
	}
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		for {
			k, ok, err := p.start()
			if err != nil {
				return nil, err
			}
			if !ok {
				return m, nil
			}
			if k.Name.Local != "key" {
				return nil, errors.New("a dict entry has no key")
			}
			var key string
			if err := p.d.DecodeElement(&key, &k); err != nil {
				return nil, errors.New("the property list is not valid XML")
			}
			v, ok, err := p.start()
			if err != nil || !ok {
				return nil, errors.New("a dict key has no value")
			}
			if m[key], err = p.value(v, depth+1); err != nil {
				return nil, err
			}
		}
	case "array":
		a := []any{}
		for {
			e, ok, err := p.start()
			if err != nil {
				return nil, err
			}
			if !ok {
				return a, nil
			}
			v, err := p.value(e, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
	case "true", "false":
		if err := p.d.Skip(); err != nil {
			return nil, errors.New("the property list is not valid XML")
		}
		return se.Name.Local == "true", nil
	case "string", "integer", "real", "data", "date":
		var s string
		if err := p.d.DecodeElement(&s, &se); err != nil {
			return nil, errors.New("the property list is not valid XML")
		}
		switch se.Name.Local {
		case "integer":
			n, err := strconv.ParseInt(trimSpace(s), 10, 64)
			if err != nil {
				return nil, errors.New("an integer in the property list is not a number")
			}
			return n, nil
		case "real":
			f, err := strconv.ParseFloat(trimSpace(s), 64)
			if err != nil {
				return nil, errors.New("a real in the property list is not a number")
			}
			return f, nil
		}
		return s, nil
	}
	return nil, errors.New("the property list has an element this reader does not know")
}

func trimSpace(s string) string { return string(bytes.TrimSpace([]byte(s))) }
