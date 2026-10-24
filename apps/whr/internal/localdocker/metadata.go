// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// Docker's projected records are bounded by the process reader. Reject duplicate
// keys before decoding into structs/maps, where encoding/json would discard the
// first value. In particular, ambiguity must never hide a mount or ownership key.
func decodeDockerRecord(dec *json.Decoder, dst any) error {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	if !utf8.Valid(raw) {
		return errors.New("invalid Docker text")
	}
	check := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueDockerValue(check, 0); err != nil {
		return err
	}
	if _, err := check.Token(); err != io.EOF {
		return errors.New("trailing Docker value")
	}
	var projectedMounts []map[string]json.RawMessage
	if _, ok := dst.(*inspection); ok {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		for key := range fields {
			if key != "Id" && key != "Name" && key != "Image" && key != "State" && key != "Labels" && key != "Mounts" {
				return errors.New("unexpected projected Docker field")
			}
		}
		var mounts []map[string]json.RawMessage
		if data, ok := fields["Mounts"]; ok {
			if err := json.Unmarshal(data, &mounts); err != nil {
				return err
			}
		}
		projectedMounts = mounts
		for _, m := range mounts {
			for key := range m {
				for _, known := range []string{"Type", "Name", "Source", "Destination", "RW"} {
					if strings.EqualFold(key, known) && key != known {
						return errors.New("ambiguous Docker mount field")
					}
				}
			}
		}
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return err
	}
	if item, ok := dst.(*inspection); ok {
		for i, m := range projectedMounts {
			value := strings.TrimSpace(string(m["RW"]))
			item.Mounts[i].readOnlyUnknown = value != "false" && value != "true"
		}
	}
	return nil
}

func uniqueDockerValue(dec *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("Docker JSON nesting limit")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			token, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return errors.New("duplicate Docker JSON key")
			}
			seen[key] = true
			if err := uniqueDockerValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := uniqueDockerValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected Docker JSON delimiter")
	}
	_, err = dec.Token()
	return err
}
