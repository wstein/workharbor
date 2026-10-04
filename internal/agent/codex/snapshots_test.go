package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
)

func TestTurnSnapshotsRejectMalformedAndUnsupportedItems(t *testing.T) {
	for _, items := range []string{
		`[true]`, `[null]`, `[{}]`,
		`[{"id":"child","type":"collabAgentToolCall","tool":"spawnAgent","status":"completed","senderThreadId":"thread","receiverThreadIds":["child-thread"],"agentsStates":{}}]`,
		`[{"id":"child","type":"subAgentActivity","agentThreadId":"child","agentPath":"child","kind":"started"}]`,
		`[{"id":"file","type":"fileChange","status":"completed","changes":[true]}]`,
		`[{"id":"message","type":"agentMessage"}]`,
		`[{"id":"message","type":"agentMessage","text":"unowned"}]`,
		`[{"id":"user","type":"userMessage","content":[true]}]`,
		`[{"id":"reason","type":"reasoning","summary":[true]}]`,
		`[{"id":"command","type":"commandExecution","command":"pwd","cwd":"/ws","status":"completed","commandActions":[true]}]`,
	} {
		for _, method := range []string{"turn/started", "turn/completed"} {
			t.Run(method+items, func(t *testing.T) {
				state := newState("thread", "turn", "synthetic-model")
				status := "completed"
				if method == "turn/started" {
					status = "inProgress"
				}
				params := `{"threadId":"thread","turn":{"id":"turn","status":"` + status + `","items":` + items + `,"error":null}}`
				if _, err := state.observe(nativeMessage(t, method, params)); !errors.Is(err, errProtocol) || state.result != nil {
					t.Errorf("unchecked snapshot accepted: %v", err)
				}
			})
		}
		t.Run("launch"+items, func(t *testing.T) {
			runner := &fixtureRunner{startTurn: make(chan struct{}), startItems: items}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := fixtureAdapter(runner).launch(ctx, fixtureSpec(), "")
			if session != nil {
				cancel()
				_, _ = session.Wait()
			}
			if !errors.Is(err, errProtocol) || session != nil {
				t.Fatalf("unchecked startup snapshot accepted: %v", err)
			}
		})
	}
}

func TestTypedItemsAndConsistentTerminalSnapshots(t *testing.T) {
	for _, item := range []string{
		`{"id":"item","type":"contextCompaction"}`,
		`{"id":"item","type":"reasoning","content":["reason"],"summary":["summary"]}`,
		`{"id":"item","type":"userMessage","content":[{"type":"text","text":"prompt","text_elements":[]}]}`,
		`{"id":"item","type":"agentMessage","text":"answer","phase":"final_answer"}`,
		`{"id":"item","type":"commandExecution","command":"pwd","cwd":"/ws","status":"inProgress","commandActions":[{"type":"unknown","command":"pwd"}]}`,
		`{"id":"item","type":"commandExecution","command":"cat file","cwd":"/ws","status":"inProgress","commandActions":[{"type":"read","command":"cat file","name":"file","path":"/ws/file"}]}`,
		`{"id":"item","type":"commandExecution","command":"ls","cwd":"/ws","status":"inProgress","commandActions":[{"type":"listFiles","command":"ls","path":null}]}`,
		`{"id":"item","type":"commandExecution","command":"rg text","cwd":"/ws","status":"inProgress","commandActions":[{"type":"search","command":"rg text","path":null,"query":"text"}]}`,
		`{"id":"item","type":"fileChange","status":"inProgress","changes":[{"path":"/ws/file","kind":{"type":"add"},"diff":"+text"}]}`,
	} {
		t.Run(item, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			params := `{"threadId":"thread","turnId":"turn","item":` + item + `}`
			if _, err := state.observe(nativeMessage(t, "item/started", params)); err != nil {
				t.Fatal(err)
			}
			params = strings.ReplaceAll(params, `"status":"inProgress"`, `"status":"completed"`)
			if _, err := state.observe(nativeMessage(t, "item/completed", params)); err != nil {
				t.Fatal(err)
			}
			item = strings.ReplaceAll(item, `"status":"inProgress"`, `"status":"completed"`)
			params = `{"threadId":"thread","turn":{"id":"turn","status":"completed","items":[` + item + `],"itemsView":"full","error":null}}`
			if _, err := state.observe(nativeMessage(t, "turn/completed", params)); err != nil || state.result == nil || state.result.Status != agent.ResultCompleted {
				t.Fatalf("consistent snapshot: %v: %v", state.result, err)
			}
		})
	}
}

func TestTypedItemsRejectMalformedAtEventIngress(t *testing.T) {
	for _, item := range []string{
		`{"id":"item","type":"userMessage","content":[true]}`,
		`{"id":"item","type":"userMessage","content":[{"type":"text","text":"prompt","text_elements":null}]}`,
		`{"id":"item","type":"reasoning","summary":null}`,
		`{"id":"item","type":"reasoning","content":[true]}`,
		`{"id":"item","type":"agentMessage","text":null}`,
		`{"id":"item","type":"agentMessage","text":"answer","phase":"future"}`,
		`{"id":"item","type":"agentMessage","text":"answer","questions":[true]}`,
		`{"id":"item","type":"commandExecution","command":"pwd","cwd":"/ws","status":"inProgress","commandActions":[true]}`,
		`{"id":"item","type":"commandExecution","command":"pwd","cwd":"/ws","status":"inProgress"}`,
		`{"id":"item","type":"contextCompaction","senderThreadId":"other"}`,
	} {
		state := newState("thread", "turn", "synthetic-model")
		params := `{"threadId":"thread","turnId":"turn","item":` + item + `}`
		if _, err := state.observe(nativeMessage(t, "item/started", params)); !errors.Is(err, errProtocol) || len(state.items) != 0 || state.itemBytes != 0 {
			t.Errorf("malformed item retained: %s: %v", item, err)
		}
	}
}

func TestSnapshotBoundsAndPartialViewsRefuse(t *testing.T) {
	for _, view := range []string{"notLoaded", "summary", "future"} {
		state := newState("thread", "turn", "synthetic-model")
		params := `{"threadId":"thread","turn":{"id":"turn","status":"completed","items":[],"itemsView":"` + view + `","error":null}}`
		if _, err := state.observe(nativeMessage(t, "turn/completed", params)); !errors.Is(err, errProtocol) || state.result != nil {
			t.Errorf("partial snapshot accepted: %s: %v", view, err)
		}
	}
	state := newState("thread", "turn", "synthetic-model")
	items := `[` + strings.Repeat(`{"id":"item","type":"contextCompaction"},`, 256) + `{"id":"item","type":"contextCompaction"}]`
	if err := state.validateSnapshot(nativeTurn{ID: "turn", Items: json.RawMessage(items)}); !errors.Is(err, errProtocol) {
		t.Fatalf("oversized snapshot accepted: %v", err)
	}
}

func TestTerminalSnapshotChecksToolPayloadAndCompletion(t *testing.T) {
	item := `{"id":"command","type":"commandExecution","command":"! false --no-network","cwd":"/ws","status":"inProgress","commandActions":[]}`
	for _, test := range []struct {
		name, status, snapshot string
		complete, wantError    bool
	}{
		{"pending-success", "completed", item, false, true},
		{"pending-interrupt", "interrupted", item, false, false},
		{"changed-command", "completed", strings.ReplaceAll(item, `! false --no-network`, `true`), true, true},
		{"changed-directory", "completed", strings.ReplaceAll(item, `/ws`, `/outside`), true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newState("thread", "turn", "synthetic-model")
			params := `{"threadId":"thread","turnId":"turn","item":` + item + `}`
			if _, err := state.observe(nativeMessage(t, "item/started", params)); err != nil {
				t.Fatal(err)
			}
			if test.complete {
				params = strings.ReplaceAll(params, `"status":"inProgress"`, `"status":"completed"`)
				if _, err := state.observe(nativeMessage(t, "item/completed", params)); err != nil {
					t.Fatal(err)
				}
				test.snapshot = strings.ReplaceAll(test.snapshot, `"status":"inProgress"`, `"status":"completed"`)
			}
			params = `{"threadId":"thread","turn":{"id":"turn","status":"` + test.status + `","items":[` + test.snapshot + `],"error":null}}`
			_, err := state.observe(nativeMessage(t, "turn/completed", params))
			if test.wantError {
				if !errors.Is(err, errProtocol) || state.result != nil {
					t.Fatalf("contradictory terminal accepted: %v", err)
				}
			} else if err != nil || state.result == nil || state.result.Status != agent.ResultStopped {
				t.Fatalf("consistent interruption: %v: %v", state.result, err)
			}
		})
	}
}

func TestTerminalSnapshotRequiresOwnedConsistentItems(t *testing.T) {
	for _, items := range []string{
		`[]`,
		`[{"id":"message","type":"agentMessage","text":"changed"}]`,
		`[{"id":"message","type":"reasoning"}]`,
		`[{"id":"message","type":"agentMessage","text":"finished"},{"id":"message","type":"agentMessage","text":"finished"}]`,
	} {
		state := newState("thread", "turn", "synthetic-model")
		text := "finished"
		state.items["message"] = nativeItem{ID: "message", Type: "agentMessage", Text: &text}
		state.completed["message"] = true
		params := `{"threadId":"thread","turn":{"id":"turn","status":"completed","items":` + items + `,"error":null}}`
		if _, err := state.observe(message{Method: "turn/completed", Params: json.RawMessage(params)}); !errors.Is(err, errProtocol) || state.result != nil {
			t.Errorf("inconsistent snapshot accepted: %s: %v", items, err)
		}
	}
}
