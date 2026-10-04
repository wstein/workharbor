package codex

import (
	"bytes"
	"encoding/json"
)

func validateChanges(raw json.RawMessage) error {
	var changes []struct {
		Path *string `json:"path"`
		Diff *string `json:"diff"`
		Kind *struct {
			Type string          `json:"type"`
			Move json.RawMessage `json:"move_path"`
		} `json:"kind"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decodeExact(raw, &changes) != nil || decoder.Decode(&changes) != nil || changes == nil || len(changes) > 256 {
		return errProtocol
	}
	for _, change := range changes {
		if change.Path == nil || *change.Path == "" || change.Diff == nil || change.Kind == nil {
			return errProtocol
		}
		switch change.Kind.Type {
		case "add", "delete":
			if change.Kind.Move != nil {
				return errProtocol
			}
		case "update":
			if change.Kind.Move != nil && !bytes.Equal(change.Kind.Move, []byte("null")) {
				var path string
				if decodeExact(change.Kind.Move, &path) != nil || path == "" {
					return errProtocol
				}
			}
		default:
			return errProtocol
		}
	}
	return nil
}
