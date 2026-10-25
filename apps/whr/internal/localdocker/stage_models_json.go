// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
)

func (t StageTarget) MarshalJSON() ([]byte, error) {
	count := 0
	var value any
	if t.Resource != nil {
		count++
		if t.Resource.Kind != "container" && t.Resource.Kind != "volume" && t.Resource.Kind != "image" {
			return nil, errors.New("invalid resource target kind")
		}
		value = t.Resource
	}
	if t.Objects != nil {
		count++
		if t.Objects.Kind != "git_objects" {
			return nil, errors.New("invalid Git object target kind")
		}
		value = t.Objects
	}
	if t.Refs != nil {
		count++
		if t.Refs.Kind != "git_refs" {
			return nil, errors.New("invalid Git ref target kind")
		}
		value = t.Refs
	}
	if t.Output != nil {
		count++
		if t.Output.Kind != "clone_output" {
			return nil, errors.New("invalid clone output target kind")
		}
		value = t.Output
	}
	if count != 1 {
		return nil, errors.New("effect target must have exactly one kind")
	}
	return json.Marshal(value)
}
func (t *StageTarget) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return errors.New("invalid effect target")
	}
	var kind string
	if json.Unmarshal(fields["kind"], &kind) != nil {
		return errors.New("missing effect target kind")
	}
	var result StageTarget
	var dst any
	switch kind {
	case "container", "volume", "image":
		result.Resource = &StageResource{}
		dst = result.Resource
	case "git_objects":
		result.Objects = &StageGitObjectsTarget{}
		dst = result.Objects
	case "git_refs":
		result.Refs = &StageGitRefsTarget{}
		dst = result.Refs
	case "clone_output":
		result.Output = &StageCloneOutputTarget{}
		dst = result.Output
	default:
		return errors.New("unknown effect target kind")
	}
	if e := decodeStageClosed(raw, dst); e != nil {
		return e
	}
	*t = result
	return nil
}
func decodeStageClosed(raw []byte, dst any) error {
	check := json.NewDecoder(bytes.NewReader(raw))
	if e := uniqueDockerValue(check, 0); e != nil {
		return errors.New("invalid or duplicate lifecycle JSON fields")
	}
	if _, e := check.Token(); e != io.EOF {
		return errors.New("trailing lifecycle JSON")
	}
	if e := stageJSONShape(raw, reflect.TypeOf(dst).Elem()); e != nil {
		return e
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
func stageJSONShape(raw []byte, typ reflect.Type) error {
	if typ.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		return stageJSONShape(raw, typ.Elem())
	}
	if typ == reflect.TypeOf(StageTarget{}) {
		var v StageTarget
		return v.UnmarshalJSON(raw)
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return errors.New("lifecycle object required")
		}
		if len(fields) != typ.NumField() {
			return errors.New("closed lifecycle fields missing or unknown")
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := f.Tag.Get("json")
			data, ok := fields[name]
			if !ok {
				return errors.New("required lifecycle field missing")
			}
			if e := stageJSONShape(data, f.Type); e != nil {
				return e
			}
		}
	case reflect.Slice:
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("lifecycle array cannot be null")
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return errors.New("lifecycle array required")
		}
		for _, item := range items {
			if e := stageJSONShape(item, typ.Elem()); e != nil {
				return e
			}
		}
	case reflect.Map:
		var entries map[string]json.RawMessage
		if json.Unmarshal(raw, &entries) != nil || entries == nil {
			return errors.New("lifecycle map required")
		}
		for _, v := range entries {
			if e := stageJSONShape(v, typ.Elem()); e != nil {
				return e
			}
		}
	default:
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("nonnullable lifecycle value")
		}
	}
	return nil
}
