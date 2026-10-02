package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
)

// stub is a stand-in for the claude CLI inside an environment: a Runner whose
// Exec plays a scripted stream-json session. It follows the shapes of spike #1
// (spikes/transcript/RESULTS.md); how the real CLI signals an unknown session
// and an exhausted quota is unverified, and the stub encodes the adapter's
// assumption for each. It implements agenttest.Scenarios.
type stub struct {
	mu       sync.Mutex
	queue    []scenario
	known    map[string]bool
	sessions int
	requests int
	calls    [][]string // the command lines it was started with
	envs     [][]string
	dirs     []string
	lines    int
}

type scenario struct {
	kind  string // finish, ask, block, auth, quota
	text  string
	tool  string
	input string
	reset time.Time
}

func newStub() *stub { return &stub{known: map[string]bool{}} }

func (s *stub) arrange(sc scenario) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, sc)
}

func (s *stub) Finish(text string)          { s.arrange(scenario{kind: "finish", text: text}) }
func (s *stub) AskApproval(tool, in string) { s.arrange(scenario{kind: "ask", tool: tool, input: in}) }
func (s *stub) Block()                      { s.arrange(scenario{kind: "block"}) }
func (s *stub) AuthExpires()                { s.arrange(scenario{kind: "auth"}) }
func (s *stub) QuotaExhausted(at time.Time) { s.arrange(scenario{kind: "quota", reset: at}) }

func (s *stub) nextRequestID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	return fmt.Sprintf("req-%d", s.requests)
}

// argsOf returns the command's arguments.
func argsOf(cmd []string) []string { return cmd }

func (s *stub) next() scenario {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return scenario{kind: "finish", text: "done"}
	}
	sc := s.queue[0]
	s.queue = s.queue[1:]
	return sc
}

func (s *stub) lastCall() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[len(s.calls)-1]
}

// argAfter returns the value after a flag, or "".
func argAfter(cmd []string, flag string) string {
	if i := slices.Index(cmd, flag); i >= 0 && i+1 < len(cmd) {
		return cmd[i+1]
	}
	return ""
}

// ctlResponse is a control_response the host wrote to the stub's stdin.
type ctlResponse struct{ id, behavior, message string }

type stubStream struct {
	chunks chan runtime.Chunk
	done   chan struct{}
	code   int
	err    error
}

func (st *stubStream) Chunks() <-chan runtime.Chunk { return st.chunks }
func (st *stubStream) Wait() (int, error)           { <-st.done; return st.code, st.err }

// Exec implements Runner.
func (s *stub) Exec(ctx context.Context, _ string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if st, ok := preflight(req, ""); ok {
		return st, nil
	}
	s.mu.Lock()
	s.calls = append(s.calls, req.Cmd)
	s.envs = append(s.envs, req.Env)
	s.dirs = append(s.dirs, req.Dir)
	s.mu.Unlock()

	st := &stubStream{chunks: make(chan runtime.Chunk, 16), done: make(chan struct{})}
	go func() {
		defer close(st.done)
		defer close(st.chunks)
		s.process(ctx, st, req)
	}()
	return st, nil
}

func (s *stub) process(ctx context.Context, st *stubStream, req runtime.ExecRequest) {
	lines := make(chan string)
	ctl := make(chan ctlResponse, 8)
	go func() {
		defer close(lines)
		defer close(ctl)
		sc := bufio.NewScanner(req.Stdin)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
		for sc.Scan() {
			var m struct {
				Type     string `json:"type"`
				Response struct {
					Subtype   string `json:"subtype"`
					RequestID string `json:"request_id"`
					Response  struct {
						Behavior string `json:"behavior"`
						Message  string `json:"message"`
					} `json:"response"`
				} `json:"response"`
				Message struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			if m.Type == "control_response" {
				select {
				case ctl <- ctlResponse{id: m.Response.RequestID, behavior: m.Response.Response.Behavior, message: m.Response.Response.Message}:
				case <-ctx.Done():
					return
				}
				continue
			}
			if len(m.Message.Content) > 0 {
				select {
				case lines <- m.Message.Content[0].Text:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	out := func(v any) {
		b, _ := json.Marshal(v)
		b = append(b, '\n')
		s.mu.Lock()
		s.lines++
		split := s.lines%2 == 0 // every other line arrives in two chunks, split anywhere
		s.mu.Unlock()
		parts := [][]byte{b}
		if split {
			parts = [][]byte{b[:len(b)/3], b[len(b)/3:]}
		}
		for _, p := range parts {
			select {
			case st.chunks <- runtime.Chunk{Stream: runtime.Stdout, Data: p}:
			case <-ctx.Done():
				return
			}
		}
	}
	cancelled := func() { st.code, st.err = 130, ctx.Err() }

	// The CLI reports nothing, not even its session, before the first message.
	select {
	case <-lines:
	case <-ctx.Done():
		cancelled()
		return
	}

	resume := argAfter(req.Cmd, "--resume")
	allowed := strings.Split(argAfter(req.Cmd, "--allowedTools"), ",")
	var id string
	s.mu.Lock()
	if resume != "" {
		if s.known[resume] {
			id = resume
		}
	} else {
		s.sessions++
		id = fmt.Sprintf("session-%d", s.sessions)
		s.known[id] = true
	}
	s.mu.Unlock()

	if id == "" { // an unknown session: an error result and no init (the adapter's assumption)
		out(map[string]any{"type": "result", "subtype": "error_during_execution", "is_error": true, "result": "No conversation found with session ID " + resume})
		st.code = 1
		return
	}

	sc := s.next()
	init := map[string]any{"type": "system", "subtype": "init", "session_id": id, "model": "claude-haiku-4-5", "apiKeySource": "none"}
	text := func(t string) map[string]any {
		return map[string]any{"type": "assistant", "session_id": id, "message": map[string]any{"content": []map[string]any{{"type": "text", "text": t}}}}
	}
	result := func(t string, isErr bool) map[string]any {
		return map[string]any{"type": "result", "subtype": "success", "is_error": isErr, "result": t, "session_id": id, "total_cost_usd": 0.0123}
	}
	window := func(util float64, reset time.Time) map[string]any {
		status := "allowed"
		if util >= 1 {
			status = "rejected" // an assumption: the real value was not observed
		}
		return map[string]any{"type": "rate_limit_event", "rate_limit_info": map[string]any{
			"status": status, "unifiedWindows": map[string]any{"five_hour": map[string]any{"utilization": util, "resetsAt": reset.Unix()}},
		}}
	}
	idle := func() { // after its result the process waits for stdin to end
		for {
			select {
			case _, ok := <-lines:
				if !ok {
					return
				}
			case <-ctx.Done():
				cancelled()
				return
			}
		}
	}

	switch sc.kind {
	case "finish":
		out(init)
		out(text(sc.text))
		out(window(0.25, time.Now().Add(3*time.Hour)))
		out(result(sc.text, false))
		idle()
	case "ask":
		out(init)
		out(map[string]any{"type": "assistant", "session_id": id, "message": map[string]any{"content": []map[string]any{
			{"type": "tool_use", "id": "tu1", "name": sc.tool, "input": map[string]any{"command": sc.input}},
		}}})
		toolResult := func(content string, isErr bool) {
			out(map[string]any{"type": "user", "session_id": id, "message": map[string]any{"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": content, "is_error": isErr},
			}}})
		}
		switch {
		case slices.Contains(argsOf(req.Cmd), "--permission-prompt-tool"):
			// manual mode: the CLI asks the host and waits for the answer to its
			// request_id; an answer to another ID is ignored (spike #7, case 6)
			reqID := s.nextRequestID()
			out(map[string]any{"type": "control_request", "request_id": reqID, "request": map[string]any{
				"subtype": "can_use_tool", "tool_name": sc.tool, "input": map[string]any{"command": sc.input}, "tool_use_id": "tu1",
			}})
		wait:
			for {
				select {
				case r, ok := <-ctl:
					if !ok { // stdin closed before an answer: the tool does not run (case 3)
						toolResult("Tool permission request failed: AbortError: Tool permission stream closed before response received", true)
						break wait
					}
					if r.id != reqID {
						continue
					}
					if r.behavior == "allow" {
						toolResult("ok", false)
					} else {
						toolResult(r.message, true)
					}
					break wait
				case <-ctx.Done():
					cancelled()
					return
				}
			}
		case slices.Contains(allowed, sc.tool):
			toolResult("ok", false)
		default:
			out(map[string]any{"type": "system", "subtype": "permission_denied", "session_id": id, "tool_name": sc.tool})
		}
		out(text("finished"))
		out(result("finished", false))
		idle()
	case "block":
		out(init)
		out(text("working"))
		for {
			select {
			case msg, ok := <-lines:
				if !ok {
					return
				}
				out(text("user: " + msg))
			case <-ctx.Done():
				cancelled()
				return
			}
		}
	case "auth":
		out(map[string]any{
			"type": "assistant", "session_id": id, "error": "authentication_failed",
			"message": map[string]any{"content": []map[string]any{{"type": "text", "text": "Not logged in · Please run /login"}}},
		})
		out(result("Not logged in · Please run /login", true))
		st.code = 1
	case "quota":
		out(init)
		out(window(1.0, sc.reset))
		out(text("Usage limit reached."))
		out(result("Usage limit reached.", false))
		idle()
	}
}
