package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/runtime"
)

type fixtureRunner struct {
	mu               sync.Mutex
	requests         []message
	startTurn        chan struct{}
	resume           string
	model            string
	steerError       bool
	approval         bool
	approvalAnswered chan message
	terminal         string
}

type fixtureStream struct {
	chunks chan runtime.Chunk
	done   chan struct{}
}

func (stream *fixtureStream) Chunks() <-chan runtime.Chunk { return stream.chunks }
func (stream *fixtureStream) Wait() (int, error)           { <-stream.done; return 0, nil }

func (runner *fixtureRunner) Exec(ctx context.Context, _ string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if len(req.Cmd) != 4 || req.Cmd[1] != "app-server" || req.Cmd[2] != "--listen" || req.Cmd[3] != "stdio://" {
		return nil, errors.New("unexpected command")
	}
	stream := &fixtureStream{chunks: make(chan runtime.Chunk, 32), done: make(chan struct{})}
	go func() {
		defer close(stream.done)
		defer close(stream.chunks)
		go func() {
			<-ctx.Done()
			if closer, ok := req.Stdin.(io.Closer); ok {
				_ = closer.Close()
			}
		}()
		send := func(value message) {
			encoded, _ := json.Marshal(value)
			select {
			case stream.chunks <- runtime.Chunk{Stream: runtime.Stdout, Data: append(encoded, '\n')}:
			case <-ctx.Done():
			}
		}
		reader := bufio.NewReader(req.Stdin)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var request message
			if json.Unmarshal(line, &request) != nil {
				return
			}
			runner.mu.Lock()
			runner.requests = append(runner.requests, request)
			runner.mu.Unlock()
			var result string
			switch request.Method {
			case "initialize":
				result = `{"userAgent":"synthetic","codexHome":"/state","platformFamily":"unix","platformOs":"linux"}`
			case "initialized":
				continue
			case "thread/start", "thread/resume":
				model := runner.model
				if model == "" {
					model = "synthetic-model"
				}
				thread := "thread"
				if runner.resume != "" {
					thread = runner.resume
				}
				result = `{"thread":{"id":"` + thread + `"},"model":"` + model + `","modelProvider":"openai","reasoningEffort":"low","cwd":"/ws","approvalPolicy":"untrusted","approvalsReviewer":"user","sandbox":{"type":"readOnly"},"instructionSources":[]}`
			case "turn/start":
				result = `{"turn":{"id":"turn","status":"inProgress","items":[],"error":null}}`
			case "turn/steer":
				if runner.steerError {
					send(message{ID: request.ID, Error: &rpcError{Code: -1, Message: "refused"}})
					continue
				}
				result = `{"turnId":"turn"}`
			case "turn/interrupt":
				result = `{}`
			default:
				if request.Method != "" {
					return
				}
				if runner.approvalAnswered != nil {
					runner.approvalAnswered <- request
				}
				result = ""
			}
			if result != "" {
				send(message{ID: request.ID, Result: json.RawMessage(result)})
			}
			if request.Method == "turn/start" {
				close(runner.startTurn)
				if runner.terminal != "" {
					errorJSON := "null"
					status := runner.terminal
					if status == "unauthorized" || status == "usageLimitExceeded" {
						errorJSON = `{"message":"synthetic","codexErrorInfo":"` + status + `"}`
						status = "failed"
					}
					send(message{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","items":[],"status":"` + status + `","error":` + errorJSON + `}}`)})
				}
				if runner.approval {
					send(message{Method: "item/started", Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","item":{"id":"command","type":"commandExecution","command":"pwd","cwd":"/ws","commandActions":[],"status":"inProgress"}}`)})
					send(message{ID: json.RawMessage(`"approval"`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","itemId":"command","startedAtMs":1,"command":"pwd","cwd":"/ws"}`)})
				}
			}
			if request.Method == "turn/interrupt" {
				send(message{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted","items":[],"error":null}}`)})
			}
		}
	}()
	return stream, nil
}

func fixtureAdapter(runner *fixtureRunner) *Adapter {
	return New(runner, Config{Bin: "/tools/codex", Model: "synthetic-model", Effort: "low"})
}

func fixtureSpec() agent.StartSpec {
	return agent.StartSpec{EnvID: "environment", Workdir: "/ws", Prompt: "synthetic prompt", Auth: agent.AuthSubscription, Approver: agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) { return agent.Approval{}, nil })}
}

func TestProductionGate(t *testing.T) {
	adapter := fixtureAdapter(nil)
	if adapter.Capabilities().Mode() != agent.ModeUnsupported {
		t.Fatal("unmeasured adapter claims native support")
	}
	for _, resume := range []string{"", "thread"} {
		var err error
		if resume == "" {
			_, err = adapter.Start(context.Background(), fixtureSpec())
		} else {
			_, err = adapter.Resume(context.Background(), fixtureSpec(), resume)
		}
		if !errors.Is(err, agent.ErrUnsupported) {
			t.Fatal(err)
		}
	}
}

func TestNativeLifecycle(t *testing.T) {
	for _, resume := range []string{"", "thread"} {
		t.Run(resume, func(t *testing.T) {
			runner := &fixtureRunner{startTurn: make(chan struct{})}
			session, err := fixtureAdapter(runner).launch(t.Context(), fixtureSpec(), resume)
			if err != nil {
				t.Fatal(err)
			}
			if session.ID() != "thread" {
				t.Fatal(session.ID())
			}
			if delivery, err := session.Instruct(t.Context(), "steer"); err != nil || delivery != agent.DeliveryNextTurn {
				t.Fatalf("%stream: %v", delivery, err)
			}
			if err := session.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, err := session.Wait()
			if err != nil || result.Status != agent.ResultStopped || result.SessionID != "thread" {
				t.Fatalf("%v: %v", result, err)
			}
			runner.mu.Lock()
			defer runner.mu.Unlock()
			want := []string{"initialize", "initialized", "thread/start", "turn/start", "turn/steer", "turn/interrupt"}
			if resume != "" {
				want[2] = "thread/resume"
			}
			for index, method := range want {
				if index >= len(runner.requests) || runner.requests[index].Method != method {
					t.Fatalf("request %d: %v", index, runner.requests)
				}
			}
		})
	}
}

func TestChangedBindingRefusesTurn(t *testing.T) {
	runner := &fixtureRunner{startTurn: make(chan struct{}), model: "changed-default"}
	if _, err := fixtureAdapter(runner).launch(t.Context(), fixtureSpec(), "thread"); !errors.Is(err, errProtocol) {
		t.Fatal(err)
	}
	select {
	case <-runner.startTurn:
		t.Fatal("turn launched with changed binding")
	default:
	}
}

func TestSteerRefusal(t *testing.T) {
	runner := &fixtureRunner{startTurn: make(chan struct{}), steerError: true}
	session, err := fixtureAdapter(runner).launch(t.Context(), fixtureSpec(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Stop(t.Context()) }()
	if delivery, err := session.Instruct(t.Context(), "steer"); err == nil || delivery != "" {
		t.Fatalf("claimed delivery %stream: %v", delivery, err)
	}
}

func TestApprovalCancelled(t *testing.T) {
	runner := &fixtureRunner{startTurn: make(chan struct{}), approval: true}
	spec := fixtureSpec()
	asked := make(chan struct{})
	spec.Approver = agent.ApproverFunc(func(ctx context.Context, _ agent.ApprovalRequest) (agent.Approval, error) {
		close(asked)
		<-ctx.Done()
		return agent.Approval{Allow: true}, nil
	})
	session, err := fixtureAdapter(runner).launch(t.Context(), spec, "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatal("approval not routed")
	}
	if err := session.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, _ = session.Wait()
	for event := range session.Events() {
		if event.Approval != nil && event.Approval.Allow {
			t.Fatal("cancelled approval allowed")
		}
	}
}

func TestNativeApprovalResponse(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny", true: "allow"}[allow], func(t *testing.T) {
			runner := &fixtureRunner{startTurn: make(chan struct{}), approval: true, approvalAnswered: make(chan message, 1)}
			spec := fixtureSpec()
			spec.Approver = agent.ApproverFunc(func(_ context.Context, request agent.ApprovalRequest) (agent.Approval, error) {
				if request.ID == "s:approval" || request.Tool != "commandExecution" {
					t.Error("missing connection-scoped approval identity")
				}
				return agent.Approval{Allow: allow, Reason: "human decision"}, nil
			})
			session, err := fixtureAdapter(runner).launch(t.Context(), spec, "")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = session.Stop(t.Context()) }()
			select {
			case response := <-runner.approvalAnswered:
				want := `{"decision":"decline"}`
				if allow {
					want = `{"decision":"accept"}`
				}
				if string(response.ID) != `"approval"` || string(response.Result) != want {
					t.Fatalf("approval response: %v", response)
				}
			case <-time.After(time.Second):
				t.Fatal("approval response missing")
			}
		})
	}
}

func TestParentCancellation(t *testing.T) {
	runner := &fixtureRunner{startTurn: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	session, err := fixtureAdapter(runner).launch(ctx, fixtureSpec(), "")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	result, err := session.Wait()
	if err != nil || result.Status != agent.ResultStopped {
		t.Fatalf("%v: %v", result, err)
	}
}

func TestNativeTerminalSession(t *testing.T) {
	for _, test := range []struct {
		status string
		want   agent.ResultStatus
	}{{"completed", agent.ResultCompleted}, {"interrupted", agent.ResultStopped}, {"unauthorized", agent.ResultAuthExpired}, {"usageLimitExceeded", agent.ResultQuotaExhausted}} {
		t.Run(test.status, func(t *testing.T) {
			runner := &fixtureRunner{startTurn: make(chan struct{}), terminal: test.status}
			session, err := fixtureAdapter(runner).launch(t.Context(), fixtureSpec(), "")
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Wait()
			if err != nil || result.Status != test.want || result.SessionID != "thread" {
				t.Fatalf("%v: %v", result, err)
			}
		})
	}
}
