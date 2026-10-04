package codex

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestApprovalRejectsCaseAliases(t *testing.T) {
	for _, fields := range []string{
		`"threadId":"wrong","ThreadId":"thread"`,
		`"ThreadId":"wrong","threadId":"thread"`,
		`"threadId":"thread","turnId":"wrong","TurnId":"turn"`,
		`"threadId":"thread","ItemId":"command"`,
		`"threadId":"thread","Kind":"command"`,
		`"threadId":"thread","GrantRoot":null`,
		`"threadId":"thread","proposedExecpolicyAmendment":["pwd"],"ProposedExecpolicyAmendment":null`,
	} {
		t.Run(fields, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			state.items["command"] = nativeItem{ID: "command", Type: "commandExecution", Command: "pwd", Cwd: "/ws"}
			request := message{ID: json.RawMessage(`1`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"turnId":"turn","itemId":"command","startedAtMs":1,"command":"pwd","cwd":"/ws",` + fields + `}`)}
			if _, err := state.approval(request); !errors.Is(err, errProtocol) {
				t.Fatalf("case alias accepted: %v", err)
			}
		})
	}
}

func TestEnvelopeRejectsCaseAliases(t *testing.T) {
	for _, raw := range []string{
		`{"Method":"turn/completed","params":{}}`,
		`{"id":1,"Id":2,"result":{}}`,
		`{"Id":2,"id":1,"result":{}}`,
		`{"id":1,"error":{"code":1,"Code":2,"message":"refused"}}`,
	} {
		if _, err := decodeMessage([]byte(raw)); !errors.Is(err, errProtocol) {
			t.Errorf("case alias accepted: %s: %v", raw, err)
		}
	}
}

func TestEventRejectsCaseAliases(t *testing.T) {
	for _, params := range []string{
		`{"threadId":"wrong","ThreadId":"thread","turn":{"id":"turn","status":"completed","items":[],"error":null}}`,
		`{"ThreadId":"wrong","threadId":"thread","turn":{"id":"turn","status":"completed","items":[],"error":null}}`,
		`{"threadId":"thread","turn":{"id":"wrong","Id":"turn","status":"completed","items":[],"error":null}}`,
	} {
		state := newState("thread", "turn", "synthetic-model")
		if _, err := state.observe(message{Method: "turn/completed", Params: json.RawMessage(params)}); !errors.Is(err, errProtocol) || state.result != nil {
			t.Errorf("identity alias accepted: %s: %v", params, err)
		}
	}
}
