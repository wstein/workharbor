package codex

import (
	"bytes"
	"encoding/json"
	"slices"
)

func itemObject(raw json.RawMessage, required, optional []string) error {
	if object(raw, required...) != nil {
		return errProtocol
	}
	var fields map[string]json.RawMessage
	if decodeExact(raw, &fields) != nil {
		return errProtocol
	}
	for key := range fields {
		if !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return errProtocol
		}
	}
	return nil
}

func decodeItem(raw json.RawMessage) (nativeItem, error) {
	var item nativeItem
	if decodeExact(raw, &item) != nil || !validID(item.ID) || object(raw, "id", "type") != nil {
		return item, errProtocol
	}
	required := []string{"id", "type"}
	var optional []string
	switch item.Type {
	case "contextCompaction":
	case "reasoning":
		optional = []string{"content", "summary"}
		if item.Content != nil {
			var content []string
			if decodeExact(item.Content, &content) != nil || content == nil {
				return item, errProtocol
			}
		}
		var fields map[string]json.RawMessage
		if decodeExact(raw, &fields) != nil || bytes.Equal(fields["summary"], []byte("null")) {
			return item, errProtocol
		}
	case "userMessage":
		required = append(required, "content")
		optional = []string{"clientId"}
		var input []struct {
			Type     string          `json:"type"`
			Text     *string         `json:"text"`
			Elements json.RawMessage `json:"text_elements"`
		}
		if decodeExact(item.Content, &input) != nil || input == nil || len(input) > 256 {
			return item, errProtocol
		}
		var contents []json.RawMessage
		if decodeExact(item.Content, &contents) != nil {
			return item, errProtocol
		}
		for index, value := range input {
			if value.Type != "text" || value.Text == nil || itemObject(contents[index], []string{"type", "text"}, []string{"text_elements"}) != nil {
				return item, errProtocol
			}
			if value.Elements != nil {
				var elements []json.RawMessage
				if decodeExact(value.Elements, &elements) != nil || elements == nil || len(elements) != 0 {
					return item, errProtocol
				}
			}
		}
	case "agentMessage":
		required = append(required, "text")
		optional = []string{"phase", "delivery", "memoryCitation", "questions"}
		if item.Text == nil {
			return item, errProtocol
		}
	case "commandExecution":
		required = append(required, "command", "cwd", "status", "commandActions")
		optional = []string{"aggregatedOutput", "durationMs", "exitCode", "processId", "pluginId", "scriptPath", "source"}
		if item.Command == "" || item.Cwd == "" || validateActions(item.Actions) != nil {
			return item, errProtocol
		}
	case "fileChange":
		required = append(required, "status", "changes")
		if validateChanges(item.Changes) != nil {
			return item, errProtocol
		}
	default:
		return item, errProtocol
	}
	if item.Type == "commandExecution" || item.Type == "fileChange" {
		if !slices.Contains([]string{"inProgress", "completed", "failed", "declined"}, item.Status) {
			return item, errProtocol
		}
	}
	var metadata struct {
		ClientID  *string         `json:"clientId"`
		Phase     *string         `json:"phase"`
		Delivery  *string         `json:"delivery"`
		Duration  *int64          `json:"durationMs"`
		ExitCode  *int32          `json:"exitCode"`
		Process   *string         `json:"processId"`
		Plugin    *string         `json:"pluginId"`
		Script    *string         `json:"scriptPath"`
		Source    *string         `json:"source"`
		Citation  json.RawMessage `json:"memoryCitation"`
		Questions json.RawMessage `json:"questions"`
	}
	if decodeExact(raw, &metadata) != nil || (metadata.Duration != nil && *metadata.Duration < 0) || (metadata.Phase != nil && !slices.Contains([]string{"commentary", "final_answer"}, *metadata.Phase)) || (metadata.Delivery != nil && *metadata.Delivery != "async") || (metadata.Source != nil && *metadata.Source != "agent") || metadata.Plugin != nil || metadata.Script != nil {
		return item, errProtocol
	}
	for _, unsupported := range []json.RawMessage{metadata.Citation, metadata.Questions} {
		if unsupported != nil && !bytes.Equal(unsupported, []byte("null")) {
			return item, errProtocol
		}
	}
	return item, itemObject(raw, required, optional)
}

func validateActions(raw json.RawMessage) error {
	var actions []struct {
		Type    string  `json:"type"`
		Command *string `json:"command"`
		Name    *string `json:"name"`
		Path    *string `json:"path"`
		Query   *string `json:"query"`
	}
	if decodeExact(raw, &actions) != nil || actions == nil || len(actions) > 256 {
		return errProtocol
	}
	var values []json.RawMessage
	if decodeExact(raw, &values) != nil {
		return errProtocol
	}
	for index, action := range actions {
		required := []string{"type", "command"}
		var optional []string
		if action.Command == nil {
			return errProtocol
		}
		switch action.Type {
		case "read":
			required = append(required, "name", "path")
		case "listFiles":
			optional = []string{"path"}
		case "search":
			optional = []string{"path", "query"}
		case "unknown":
		default:
			return errProtocol
		}
		if itemObject(values[index], required, optional) != nil {
			return errProtocol
		}
	}
	return nil
}

func (state *state) validateSnapshot(turn nativeTurn) error {
	var items []json.RawMessage
	if decodeExact(turn.Items, &items) != nil || items == nil || len(items) > 256 || len(items) != len(state.items) || (turn.View != "" && turn.View != "full") {
		return errProtocol
	}
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		item, err := decodeItem(raw)
		prior, owned := state.items[item.ID]
		if err != nil || !owned || seen[item.ID] || prior.Type != item.Type || (turn.Status == "completed" && !state.completed[item.ID]) {
			return errProtocol
		}
		seen[item.ID] = true
		encoded, _ := json.Marshal(item)
		previous, _ := json.Marshal(prior)
		if !bytes.Equal(encoded, previous) {
			return errProtocol
		}
	}
	return nil
}
