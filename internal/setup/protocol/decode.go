package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const maxDepth = 4

// Decode parses one line (without its newline) strictly and returns the
// validated entry. It refuses invalid UTF-8, duplicate keys, null, floats,
// unknown fields, trailing data, an unknown version, values outside their sets,
// a non-UTC or fractional time, and any line whose canonical re-encoding is not
// byte-identical to the input.
func Decode(line []byte) (Entry, error) {
	if len(line) > MaxLine {
		return Entry{}, ErrTooLarge
	}
	if !utf8.Valid(line) {
		return Entry{}, fmt.Errorf("%w: invalid UTF-8", ErrSyntax)
	}
	if err := walkDocument(line); err != nil {
		return Entry{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var e Entry
	if err := dec.Decode(&e); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return Entry{}, fmt.Errorf("%w: %w", ErrUnknownField, err)
		}
		return Entry{}, fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	canon, err := Encode(e)
	if err != nil {
		return Entry{}, err
	}
	if !bytes.Equal(canon, line) {
		return Entry{}, ErrNotCanonical
	}
	return e, nil
}

func walkDocument(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("%w: a line must be an object", ErrSyntax)
	}
	if err := walkObject(dec, 1); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrTrailing
	}
	return nil
}

func walkValue(dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	switch t := tok.(type) {
	case nil:
		return ErrNull
	case json.Number:
		if strings.ContainsAny(t.String(), ".eE") {
			return ErrFloat
		}
	case json.Delim:
		if depth >= maxDepth {
			return fmt.Errorf("%w: nesting too deep", ErrSyntax)
		}
		if t == '{' {
			return walkObject(dec, depth+1)
		}
		return walkArray(dec, depth+1)
	}
	return nil
}

func walkObject(dec *json.Decoder, depth int) error {
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrSyntax, err)
		}
		k, _ := kt.(string)
		if seen[k] {
			return fmt.Errorf("%w: %q", ErrDuplicateKey, k)
		}
		seen[k] = true
		if err := walkValue(dec, depth); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	return nil
}

func walkArray(dec *json.Decoder, depth int) error {
	for dec.More() {
		if err := walkValue(dec, depth); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	return nil
}

// Chain decodes a whole protocol file and checks how its lines relate: the
// first line has no prev, every later line's prev is LineDigest of the line
// before, a run starts with run.start at seq 1, seq counts up by one inside a
// run, a run keeps its id, account, cmd, phase and build identity, and nothing
// follows run.end but a new run. It returns the entries decoded before the
// first fault together with the error, which names the line. A file whose last
// line lacks its newline reports ErrCutOff; a line a crash cut (and a later
// run set apart with a newline) reports its decode error at that line.
func Chain(data []byte) ([]Entry, error) {
	var out []Entry
	var prevLine []byte
	rest := data
	for n := 1; len(rest) > 0; n++ {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			return out, fmt.Errorf("line %d: %w", n, ErrCutOff)
		}
		line := rest[:i]
		rest = rest[i+1:]
		e, err := Decode(line)
		if err != nil {
			return out, fmt.Errorf("line %d: %w", n, err)
		}
		if err := checkLink(out, e, prevLine); err != nil {
			return out, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, e)
		prevLine = line
	}
	return out, nil
}

func checkLink(done []Entry, e Entry, prevLine []byte) error {
	if len(done) == 0 {
		if e.Prev != "" {
			return fmt.Errorf("%w: the first line has a prev", ErrChain)
		}
	} else if e.Prev != LineDigest(prevLine) {
		return fmt.Errorf("%w: prev does not match the line before", ErrChain)
	}
	if e.Event == EventRunStart {
		if e.Seq != 1 {
			return fmt.Errorf("%w: run.start must be seq 1", ErrRunOrder)
		}
		if len(done) > 0 {
			if last := done[len(done)-1]; last.Run == e.Run {
				return fmt.Errorf("%w: run id reused", ErrRunOrder)
			}
		}
		return nil
	}
	if len(done) == 0 {
		return fmt.Errorf("%w: a run starts with run.start", ErrRunOrder)
	}
	last := done[len(done)-1]
	switch {
	case last.Event == EventRunEnd:
		return fmt.Errorf("%w: nothing follows run.end but a new run", ErrRunOrder)
	case e.Run != last.Run, e.Account != last.Account, e.Cmd != last.Cmd, e.Phase != last.Phase, e.Whr != last.Whr:
		return fmt.Errorf("%w: a run changed its identity", ErrRunOrder)
	case e.Seq != last.Seq+1:
		return fmt.Errorf("%w: seq must count up by one", ErrRunOrder)
	}
	return nil
}
