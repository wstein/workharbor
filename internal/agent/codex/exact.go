package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

func decodeExact(raw []byte, target any) error {
	if len(raw) > maxMessage || !utf8.Valid(raw) {
		return errProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if uniqueJSON(decoder, 0) != nil {
		return errProtocol
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errProtocol
	}
	if exactFields(raw, reflect.TypeOf(target)) != nil || json.Unmarshal(raw, target) != nil {
		return errProtocol
	}
	return nil
}

func exactFields(raw json.RawMessage, shape reflect.Type) error {
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	if bytes.Equal(raw, []byte("null")) || shape == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	switch shape.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return errProtocol
		}
		for index := range shape.NumField() {
			field := shape.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" {
				name = field.Name
			}
			for key, value := range fields {
				if key != name && strings.EqualFold(key, name) {
					return errProtocol
				}
				if key == name && exactFields(value, field.Type) != nil {
					return errProtocol
				}
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return errProtocol
		}
		for _, value := range values {
			if exactFields(value, shape.Elem()) != nil {
				return errProtocol
			}
		}
	}
	return nil
}
