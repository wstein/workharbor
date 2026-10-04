package codex

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
)

func nativeMessage(t *testing.T, method, params string) message {
	t.Helper()
	result, err := decodeMessage([]byte(`{"method":"` + method + `","params":` + params + `}`))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTerminalResults(t *testing.T) {
	for _, test := range []struct {
		status, info string
		want         agent.ResultStatus
	}{
		{"completed", "null", agent.ResultCompleted},
		{"interrupted", "null", agent.ResultStopped},
		{"failed", `"unauthorized"`, agent.ResultAuthExpired},
		{"failed", `"usageLimitExceeded"`, agent.ResultQuotaExhausted},
		{"failed", `"rateLimitExceeded"`, agent.ResultFailed},
		{"failed", `"other"`, agent.ResultFailed},
	} {
		t.Run(test.status+test.info, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			params := `{"threadId":"thread","turn":{"id":"turn","items":[],"status":"` + test.status + `","error":null}}`
			if test.status == "failed" {
				params = `{"threadId":"thread","turn":{"id":"turn","items":[],"status":"failed","error":{"message":"untrusted","codexErrorInfo":` + test.info + `}}}`
			}
			_, err := state.observe(nativeMessage(t, "turn/completed", params))
			if err != nil || state.result == nil || state.result.Status != test.want {
				t.Fatalf("result %v: %v", state.result, err)
			}
			if _, err := state.observe(nativeMessage(t, "turn/completed", params)); !errors.Is(err, errProtocol) {
				t.Fatal("terminal replay accepted")
			}
		})
	}
}

func TestMalformedEvents(t *testing.T) {
	for _, test := range []struct{ method, params string }{
		{"turn/completed", `{"threadId":"wrong","turn":{"id":"turn","status":"completed","items":[],"error":null}}`},
		{"turn/completed", `{"threadId":"thread","turn":{"id":"wrong","status":"completed","items":[],"error":null}}`},
		{"turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"inProgress","items":[],"error":null}}`},
		{"turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed","items":[],"error":{"message":"contradiction"}}}`},
		{"item/agentMessage/delta", `{"threadId":"thread","turnId":"turn","itemId":"missing","delta":"text"}`},
		{"item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"child","type":"collabAgentToolCall"}}`},
		{"thread/tokenUsage/updated", `{"threadId":"thread","turnId":"turn","tokenUsage":{"last":{"inputTokens":-1}}}`},
		{"future/required", `{}`},
	} {
		t.Run(test.method+test.params, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			if _, err := state.observe(nativeMessage(t, test.method, test.params)); !errors.Is(err, errProtocol) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

func TestTypedUsage(t *testing.T) {
	state := newState("thread", "turn", "synthetic-model")
	params := `{"threadId":"thread","turnId":"turn","tokenUsage":{"last":{"inputTokens":100,"outputTokens":20,"cachedInputTokens":60,"reasoningOutputTokens":5,"totalTokens":120},"total":{"inputTokens":100,"outputTokens":20,"cachedInputTokens":60,"reasoningOutputTokens":5,"totalTokens":120}}}`
	events, err := state.observe(nativeMessage(t, "thread/tokenUsage/updated", params))
	if err != nil || len(events) != 1 {
		t.Fatalf("%v: %v", events, err)
	}
	usage := events[0].Usage
	if usage == nil || usage.Tokens.Input != 40 || usage.Tokens.Output != 20 || usage.Tokens.CacheRead != 60 || usage.Cost != nil || usage.Windows != nil {
		t.Fatalf("usage %v", usage)
	}
	if _, err := state.observe(nativeMessage(t, "thread/tokenUsage/updated", params)); !errors.Is(err, errProtocol) {
		t.Fatal("duplicate turn usage accepted")
	}
}

func TestApprovalCeiling(t *testing.T) {
	for _, change := range []string{`"grantRoot":"/"`, `"threadId":"wrong"`, `"turnId":"wrong"`, `"itemId":"missing"`, `"unexpected":true`} {
		t.Run(change, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			state.items["file"] = nativeItem{ID: "file", Type: "fileChange", Changes: json.RawMessage(`[{"path":"/ws/file","kind":{"type":"add"},"diff":"+text"}]`)}
			params := map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "file", "startedAtMs": 1}
			var override map[string]any
			_ = json.Unmarshal([]byte("{"+change+"}"), &override)
			for key, value := range override {
				params[key] = value
			}
			encoded, _ := json.Marshal(params)
			request := nativeMessage(t, "item/fileChange/requestApproval", string(encoded))
			request.ID = json.RawMessage(`1`)
			if _, err := state.approval(request); !errors.Is(err, errProtocol) {
				t.Fatalf("widening accepted: %v", err)
			}
		})
	}
}

func TestApprovalReplay(t *testing.T) {
	state := newState("thread", "turn", "synthetic-model")
	state.items["command"] = nativeItem{ID: "command", Type: "commandExecution", Command: "pwd", Cwd: "/ws"}
	request := nativeMessage(t, "item/commandExecution/requestApproval", `{"threadId":"thread","turnId":"turn","itemId":"command","startedAtMs":1,"command":"pwd","cwd":"/ws"}`)
	request.ID = json.RawMessage(`"native-request"`)
	if _, err := state.approval(request); err != nil {
		t.Fatal(err)
	}
	if _, err := state.approval(request); !errors.Is(err, errProtocol) {
		t.Fatal("replayed approval was accepted")
	}
}

func TestCommandPolicyAmendments(t *testing.T) {
	for _, extra := range []string{`"proposedExecpolicyAmendment":["pwd"]`, `"proposedNetworkPolicyAmendments":[]`, `"networkApprovalContext":{}`, `"kind":"stdin"`, `"command":"changed"`} {
		t.Run(extra, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			state.items["command"] = nativeItem{ID: "command", Type: "commandExecution", Command: "pwd", Cwd: "/ws"}
			var params map[string]any
			_ = json.Unmarshal([]byte(`{"threadId":"thread","turnId":"turn","itemId":"command","startedAtMs":1,"command":"pwd","cwd":"/ws"}`), &params)
			var override map[string]any
			_ = json.Unmarshal([]byte("{"+extra+"}"), &override)
			for key, value := range override {
				params[key] = value
			}
			encoded, _ := json.Marshal(params)
			request := nativeMessage(t, "item/commandExecution/requestApproval", string(encoded))
			request.ID = json.RawMessage(`1`)
			if _, err := state.approval(request); !errors.Is(err, errProtocol) {
				t.Fatalf("accepted widening: %v", err)
			}
		})
	}
}

func TestStreamedMessageResult(t *testing.T) {
	state := newState("thread", "turn", "synthetic-model")
	for _, test := range []struct {
		method, params string
		count          int
	}{
		{"item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"message","type":"agentMessage","text":""}}`, 0},
		{"item/agentMessage/delta", `{"threadId":"thread","turnId":"turn","itemId":"message","delta":"finished"}`, 1},
		{"item/completed", `{"threadId":"thread","turnId":"turn","item":{"id":"message","type":"agentMessage","text":"finished"}}`, 0},
		{"turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed","items":[],"error":null}}`, 0},
	} {
		events, err := state.observe(nativeMessage(t, test.method, test.params))
		if err != nil || len(events) != test.count {
			t.Fatalf("%s: %v, %v", test.method, events, err)
		}
	}
	if state.result == nil || state.result.Text != "finished" {
		t.Fatal(state.result)
	}
}
