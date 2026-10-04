package codex

import (
	"encoding/json"
	"testing"
)

func TestApprovalPreservesCommandDisplay(t *testing.T) {
	for _, command := range []string{"pwd", "! test -e 'do not approve'", "printf '%s\\n' \"$HOME\";\nfalse --no-network", "echo `false` $(false)"} {
		t.Run(command, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			cwd := "/ws/space and 'quotes'%"
			state.items["command"] = nativeItem{ID: "command", Type: "commandExecution", Command: command, Cwd: cwd}
			params, err := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "command", "startedAtMs": 1, "command": command, "cwd": cwd})
			if err != nil {
				t.Fatal(err)
			}
			request := message{ID: json.RawMessage(`"approval"`), Method: "item/commandExecution/requestApproval", Params: params}
			approval, err := state.approval(request)
			if err != nil || approval.Input != "cwd: "+cwd+"\ncommand: "+command {
				t.Fatalf("approval display %q: %v", approval.Input, err)
			}
		})
	}
}
