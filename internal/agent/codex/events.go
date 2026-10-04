package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/agent"
)

type nativeItem struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             *string         `json:"text,omitempty"`
	Command          string          `json:"command,omitempty"`
	Cwd              string          `json:"cwd,omitempty"`
	Status           string          `json:"status,omitempty"`
	Changes          json.RawMessage `json:"changes,omitempty"`
	AggregatedOutput *string         `json:"aggregatedOutput,omitempty"`
}

type nativeTurn struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Items  json.RawMessage `json:"items"`
	Error  *nativeError    `json:"error"`
}

type nativeError struct {
	Message *string         `json:"message"`
	Info    json.RawMessage `json:"codexErrorInfo"`
}

type state struct {
	thread    string
	turn      string
	model     string
	items     map[string]nativeItem
	completed map[string]bool
	requests  map[string]bool
	result    *agent.Result
	usageSeen bool
	text      string
	streamed  map[string]bool
	itemBytes int
}

func newState(thread, turn, model string) *state {
	return &state{thread: thread, turn: turn, model: model, items: make(map[string]nativeItem), completed: make(map[string]bool), requests: make(map[string]bool), streamed: make(map[string]bool)}
}

func object(raw json.RawMessage, required ...string) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errProtocol
	}
	for _, key := range required {
		if value, ok := fields[key]; !ok || bytes.Equal(value, []byte("null")) {
			return errProtocol
		}
	}
	return nil
}

func validID(value string) bool { return value != "" && len(value) <= 256 }

func (state *state) event(kind agent.EventKind) agent.Event {
	return agent.Event{Kind: kind, At: time.Now(), SessionID: state.thread}
}

func (state *state) observe(value message) ([]agent.Event, error) {
	if state.result != nil || value.ID != nil {
		return nil, errProtocol
	}
	var params struct {
		Thread string       `json:"threadId"`
		TurnID string       `json:"turnId"`
		Turn   nativeTurn   `json:"turn"`
		Item   nativeItem   `json:"item"`
		ItemID string       `json:"itemId"`
		Delta  *string      `json:"delta"`
		Error  *nativeError `json:"error"`
	}
	if json.Unmarshal(value.Params, &params) != nil {
		return nil, errProtocol
	}
	if params.Thread != state.thread {
		return nil, errProtocol
	}
	if value.Method == "turn/started" || value.Method == "turn/completed" {
		if params.Turn.ID != state.turn || object(value.Params, "turn") != nil || len(params.Turn.Items) == 0 || params.Turn.Items[0] != '[' {
			return nil, errProtocol
		}
	} else if params.TurnID != state.turn {
		return nil, errProtocol
	}
	switch value.Method {
	case "turn/started":
		if params.Turn.Status != "inProgress" {
			return nil, errProtocol
		}
		return nil, nil
	case "turn/completed":
		if params.Turn.Items == nil || params.Turn.Items[0] != '[' {
			return nil, errProtocol
		}
		var status agent.ResultStatus
		switch params.Turn.Status {
		case "completed":
			if params.Turn.Error != nil {
				return nil, errProtocol
			}
			status = agent.ResultCompleted
		case "interrupted":
			status = agent.ResultStopped
		case "failed":
			if params.Turn.Error == nil || params.Turn.Error.Message == nil {
				return nil, errProtocol
			}
			status = errorStatus(params.Turn.Error)
		default:
			return nil, errProtocol
		}
		state.result = &agent.Result{Status: status, SessionID: state.thread, Text: state.text}
		return statusEvents(state, status), nil
	case "error":
		if params.Error == nil || params.Error.Message == nil || object(value.Params, "willRetry") != nil {
			return nil, errProtocol
		}
		status := errorStatus(params.Error)
		if status == agent.ResultAuthExpired || status == agent.ResultQuotaExhausted {
			state.result = &agent.Result{Status: status, SessionID: state.thread}
			return statusEvents(state, status), nil
		}
		return []agent.Event{state.event(agent.EventError)}, nil
	case "item/started", "item/completed":
		if object(value.Params, "item") != nil || !validID(params.Item.ID) {
			return nil, errProtocol
		}
		item := params.Item
		if state.completed[item.ID] {
			return nil, errProtocol
		}
		prior, exists := state.items[item.ID]
		if value.Method == "item/started" {
			if exists || len(state.items) >= 256 {
				return nil, errProtocol
			}
		} else if !exists || prior.Type != item.Type {
			return nil, errProtocol
		}
		itemJSON, _ := json.Marshal(item)
		priorJSON, _ := json.Marshal(prior)
		itemBytes := state.itemBytes + len(itemJSON)
		if exists {
			itemBytes -= len(priorJSON)
		}
		if itemBytes > 4<<20 {
			return nil, errProtocol
		}
		state.itemBytes = itemBytes
		event := state.event(agent.EventMessage)
		switch item.Type {
		case "userMessage", "reasoning", "contextCompaction":
			state.items[item.ID] = item
			if value.Method == "item/completed" {
				state.completed[item.ID] = true
			}
			return nil, nil
		case "agentMessage":
			if item.Text == nil {
				return nil, errProtocol
			}
			if value.Method == "item/started" {
				state.items[item.ID] = item
				return nil, nil
			}
			state.text = *item.Text
			if state.streamed[item.ID] {
				state.completed[item.ID] = true
				return nil, nil
			}
			event.Text = boundedText(*item.Text)
		case "commandExecution":
			if item.Command == "" || item.Cwd == "" || item.Status == "" {
				return nil, errProtocol
			}
			event.Tool = "commandExecution"
			event.Input = boundedText(item.Command)
		case "fileChange":
			if len(item.Changes) == 0 || item.Changes[0] != '[' || item.Status == "" {
				return nil, errProtocol
			}
			event.Tool = "fileChange"
			event.Input = boundedText(string(item.Changes))
		default:
			return nil, errProtocol
		}
		if event.Tool != "" {
			if value.Method == "item/started" && item.Status != "inProgress" {
				return nil, errProtocol
			}
			if value.Method == "item/completed" && item.Status != "completed" && item.Status != "failed" && item.Status != "declined" {
				return nil, errProtocol
			}
			if exists && (prior.Command != item.Command || prior.Cwd != item.Cwd || !bytes.Equal(prior.Changes, item.Changes)) {
				return nil, errProtocol
			}
			event.Kind = agent.EventToolCall
			if value.Method == "item/completed" {
				event.Kind = agent.EventToolResult
				event.Text = item.Status
			}
		}
		state.items[item.ID] = item
		if value.Method == "item/completed" {
			state.completed[item.ID] = true
		}
		return []agent.Event{event}, nil
	case "item/agentMessage/delta":
		item, exists := state.items[params.ItemID]
		if !exists || item.Type != "agentMessage" || state.completed[params.ItemID] || params.Delta == nil {
			return nil, errProtocol
		}
		event := state.event(agent.EventMessage)
		event.Text = boundedText(*params.Delta)
		state.streamed[params.ItemID] = true
		return []agent.Event{event}, nil
	case "thread/tokenUsage/updated":
		return state.usage(value.Params)
	default:
		return nil, errProtocol
	}
}

func boundedText(text string) string {
	limit := min(len(text), 64<<10)
	for !utf8.ValidString(text[:limit]) {
		limit--
	}
	return text[:limit]
}

func errorStatus(native *nativeError) agent.ResultStatus {
	var code string
	_ = json.Unmarshal(native.Info, &code)
	switch code {
	case "unauthorized":
		return agent.ResultAuthExpired
	case "usageLimitExceeded":
		return agent.ResultQuotaExhausted
	default:
		return agent.ResultFailed
	}
}

func statusEvents(state *state, status agent.ResultStatus) []agent.Event {
	switch status {
	case agent.ResultAuthExpired:
		return []agent.Event{state.event(agent.EventAuthExpired)}
	case agent.ResultQuotaExhausted:
		return []agent.Event{state.event(agent.EventQuotaExhausted)}
	default:
		return nil
	}
}

func (state *state) usage(raw json.RawMessage) ([]agent.Event, error) {
	if state.usageSeen {
		return nil, errProtocol
	}
	var params struct {
		Usage struct {
			Last  json.RawMessage `json:"last"`
			Total json.RawMessage `json:"total"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return nil, errProtocol
	}
	var last struct {
		Input      int64 `json:"inputTokens"`
		Output     int64 `json:"outputTokens"`
		Cached     int64 `json:"cachedInputTokens"`
		CacheWrite int64 `json:"cacheWriteInputTokens"`
		Reasoning  int64 `json:"reasoningOutputTokens"`
		Total      int64 `json:"totalTokens"`
	}
	for _, counts := range []json.RawMessage{params.Usage.Total, params.Usage.Last} {
		last.Input, last.Output, last.Cached, last.CacheWrite, last.Reasoning, last.Total = 0, 0, 0, 0, 0, 0
		if object(counts, "inputTokens", "outputTokens", "cachedInputTokens", "reasoningOutputTokens", "totalTokens") != nil || json.Unmarshal(counts, &last) != nil {
			return nil, errProtocol
		}
		if last.Input < 0 || last.Output < 0 || last.Cached < 0 || last.CacheWrite < 0 || last.Reasoning < 0 || last.Total < 0 || last.Input > agent.MaxTokensPerTurn || last.Output > agent.MaxTokensPerTurn || last.Cached > last.Input || last.Reasoning > last.Output || last.Total > agent.MaxTokensPerTurn || last.Total != last.Input+last.Output {
			return nil, errProtocol
		}
	}
	usage := &agent.Usage{Model: state.model, Tokens: &agent.TokenCounts{Input: last.Input - last.Cached, Output: last.Output, CacheRead: last.Cached, CacheWrite: last.CacheWrite}}
	if usage.Validate() != nil {
		return nil, errProtocol
	}
	state.usageSeen = true
	event := state.event(agent.EventUsage)
	event.Usage = usage
	return []agent.Event{event}, nil
}

func (state *state) approval(value message) (agent.ApprovalRequest, error) {
	key, err := requestID(value.ID)
	if err != nil || state.requests[key] || len(state.requests) >= maxRequests || state.result != nil {
		return agent.ApprovalRequest{}, errProtocol
	}
	state.requests[key] = true
	var params struct {
		Thread           string          `json:"threadId"`
		Turn             string          `json:"turnId"`
		Item             string          `json:"itemId"`
		Started          *int64          `json:"startedAtMs"`
		ApprovalID       *string         `json:"approvalId"`
		Command          *string         `json:"command"`
		Cwd              *string         `json:"cwd"`
		Actions          json.RawMessage `json:"commandActions"`
		Kind             string          `json:"kind"`
		Reason           *string         `json:"reason"`
		Environment      *string         `json:"environmentId"`
		GrantRoot        *string         `json:"grantRoot"`
		Network          json.RawMessage `json:"networkApprovalContext"`
		ExecAmendment    json.RawMessage `json:"proposedExecpolicyAmendment"`
		NetworkAmendment json.RawMessage `json:"proposedNetworkPolicyAmendments"`
	}
	decoder := json.NewDecoder(bytes.NewReader(value.Params))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&params) != nil || params.Thread != state.thread || params.Turn != state.turn || params.Started == nil || *params.Started < 0 {
		return agent.ApprovalRequest{}, errProtocol
	}
	item, exists := state.items[params.Item]
	if !exists || state.completed[params.Item] || params.GrantRoot != nil || params.Environment != nil {
		return agent.ApprovalRequest{}, errProtocol
	}
	for _, raw := range []json.RawMessage{params.Network, params.ExecAmendment, params.NetworkAmendment} {
		if len(raw) > 0 && string(raw) != "null" {
			return agent.ApprovalRequest{}, errProtocol
		}
	}
	req := agent.ApprovalRequest{ID: key, Tool: item.Type}
	switch value.Method {
	case "item/commandExecution/requestApproval":
		if item.Type != "commandExecution" || params.Command == nil || params.Cwd == nil || *params.Command != item.Command || *params.Cwd != item.Cwd || (params.Kind != "" && params.Kind != "command") {
			return agent.ApprovalRequest{}, errProtocol
		}
		req.Input = fmt.Sprintf("cwd: %state\ncommand: %state", item.Cwd, item.Command)
	case "item/fileChange/requestApproval":
		if item.Type != "fileChange" || len(item.Changes) == 0 || params.Command != nil || params.Cwd != nil || params.Kind != "" {
			return agent.ApprovalRequest{}, errProtocol
		}
		req.Input = string(item.Changes)
	default:
		return agent.ApprovalRequest{}, errProtocol
	}
	return req, nil
}
