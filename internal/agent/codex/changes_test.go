package codex

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestFileChangesRejectMalformedBeforeRetentionAndApproval(t *testing.T) {
	for _, changes := range []string{
		`[true]`, `[null]`, `[{}]`,
		`[{"path":"/ws/file","kind":{"type":"add"}}]`,
		`[{"path":"/ws/file","diff":"+text"}]`,
		`[{"kind":{"type":"add"},"diff":"+text"}]`,
		`[{"path":null,"kind":{"type":"add"},"diff":"+text"}]`,
		`[{"path":"/ws/file","kind":{"type":"future"},"diff":"+text"}]`,
		`[{"path":"/ws/file","kind":{"type":"update","move_path":true},"diff":"+text"}]`,
		`[{"path":"/ws/file","kind":{"type":"add","move_path":"/outside"},"diff":"+text"}]`,
		`[{"path":"/ws/file","Path":"/outside","kind":{"type":"add"},"diff":"+text"}]`,
	} {
		t.Run(changes, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			item := `{"id":"file","type":"fileChange","status":"inProgress","changes":` + changes + `}`
			if _, err := state.observe(nativeMessage(t, "item/started", `{"threadId":"thread","turnId":"turn","item":`+item+`}`)); !errors.Is(err, errProtocol) || len(state.items) != 0 || state.itemBytes != 0 {
				t.Errorf("malformed change retained: %v", err)
			}
			state.items["file"] = nativeItem{ID: "file", Type: "fileChange", Changes: json.RawMessage(changes)}
			request := nativeMessage(t, "item/fileChange/requestApproval", `{"threadId":"thread","turnId":"turn","itemId":"file","startedAtMs":1}`)
			request.ID = json.RawMessage(`1`)
			if _, err := state.approval(request); !errors.Is(err, errProtocol) {
				t.Errorf("malformed change reached approval: %v", err)
			}
		})
	}
}

func TestFileChangesPreserveValidRawApproval(t *testing.T) {
	for _, kind := range []string{`{"type":"add"}`, `{"type":"delete"}`, `{"type":"update"}`, `{"type":"update","move_path":null}`, `{"type":"update","move_path":"/ws/moved"}`} {
		state := newState("thread", "turn", "synthetic-model")
		changes := `[ {"path":"/ws/quoted 'file'","kind":` + kind + `,"diff":"-!do not run\\n+text"} ]`
		_, err := state.observe(nativeMessage(t, "item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"file","type":"fileChange","status":"inProgress","changes":`+changes+`}}`))
		if err != nil {
			t.Fatal(err)
		}
		request := nativeMessage(t, "item/fileChange/requestApproval", `{"threadId":"thread","turnId":"turn","itemId":"file","startedAtMs":1}`)
		request.ID = json.RawMessage(`1`)
		approval, err := state.approval(request)
		if err != nil || approval.Input != changes {
			t.Fatalf("raw change display %q: %v", approval.Input, err)
		}
	}
}
