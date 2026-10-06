package answers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Decode parses data strictly and returns a validated File. It refuses a file
// over 64 KiB, invalid UTF-8, duplicate keys at any level, null, floats,
// unknown fields at the top and per entry, trailing data, an unknown version, a
// host phase and every other violation of the format.
func Decode(data []byte) (File, error) {
	if len(data) > MaxBytes {
		return File{}, ErrTooLarge
	}
	if !utf8.Valid(data) {
		return File{}, fmt.Errorf("%w: invalid UTF-8", ErrSyntax)
	}
	if err := walkDocument(data); err != nil {
		return File{}, err
	}
	// Version first, so an unknown v is reported as such whatever else changed.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return File{}, fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	if string(bytes.TrimSpace(probe["v"])) != "1" {
		return File{}, ErrUnknownVersion
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return File{}, fmt.Errorf("%w: %w", ErrUnknownField, err)
		}
		return File{}, fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	if f.Answers == nil {
		return File{}, fmt.Errorf("%w: answers is missing", ErrBadAnswer)
	}
	if err := f.Validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

// Exactly these lowercase keys exist. encoding/json matches field names
// case-insensitively (with Unicode folding) and lets a later duplicate win, so
// the walker, not the struct decoder, decides which keys are known.
var (
	topKeys   = map[string]bool{"schema": true, "v": true, "whr": true, "phase": true, "account": true, "answers": true}
	entryKeys = map[string]bool{"step": true, "fix": true, "answer": true}
)

// walkDocument reads every token once: the top level must be an object, every
// key must be one of the exact known keys and appear once, answers must be an
// array of objects, and null and non-integer numbers are refused. Data after
// the document is refused too.
func walkDocument(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := expectDelim(dec, '{'); err != nil {
		return err
	}
	if err := walkObject(dec, topKeys, func(key string) error {
		if key != "answers" {
			return walkScalar(dec)
		}
		if err := expectDelim(dec, '['); err != nil {
			return err
		}
		for dec.More() {
			if err := expectDelim(dec, '{'); err != nil {
				return err
			}
			if err := walkObject(dec, entryKeys, func(string) error { return walkScalar(dec) }); err != nil {
				return err
			}
		}
		_, err := dec.Token() // ']'
		return err
	}); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrTrailing
	}
	return nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	switch t := tok.(type) {
	case nil:
		return ErrNull
	case json.Delim:
		if t == want {
			return nil
		}
	}
	return fmt.Errorf("%w: expected %q", ErrSyntax, string(want))
}

// walkObject reads the members of an object whose '{' is consumed.
func walkObject(dec *json.Decoder, known map[string]bool, value func(key string) error) error {
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrSyntax, err)
		}
		k, _ := kt.(string)
		switch {
		case !known[k]:
			return fmt.Errorf("%w: %q", ErrUnknownField, k)
		case seen[k]:
			return fmt.Errorf("%w: %q", ErrDuplicateKey, k)
		}
		seen[k] = true
		if err := value(k); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // '}'
		return fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	return nil
}

// walkScalar reads one string, bool or integer.
func walkScalar(dec *json.Decoder) error {
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
		return fmt.Errorf("%w: unexpected %q", ErrSyntax, string(t))
	}
	return nil
}

// Encode returns the saved form of f: 2-space indent, trailing newline,
// entries sorted by step. It validates f first.
func Encode(f File) ([]byte, error) {
	f.Answers = append([]Entry{}, f.Answers...)
	f.sortEntries()
	if err := f.Validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
