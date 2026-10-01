package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Claude drives Claude Code headless over its stream-json protocol.
type Claude struct {
	Bin, Dir, Model, Tools string
}

// Session is one running agent process. The process stays alive across turns.
type Session struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	hub   *Hub
	mu    sync.Mutex
	done  chan struct{}
}

// Start launches the agent. A non-empty resumeID resumes that agent session,
// which is how a restarted supervisor recovers (design 5.3).
func (c Claude) Start(hub *Hub, resumeID string) (*Session, error) {
	args := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--permission-prompts", "none",
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if c.Tools != "" {
		args = append(args, "--allowedTools", c.Tools)
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	cmd := exec.Command(c.Bin, args...) //nolint:gosec // spike, flags are fixed
	cmd.Dir = c.Dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &Session{cmd: cmd, stdin: stdin, hub: hub, done: make(chan struct{})}
	hub.Publish(Event{Kind: "status", Text: "starting"})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.pump(stdout) }()
	go func() { defer wg.Done(); s.pumpErr(stderr) }()
	go func() {
		wg.Wait()
		err := cmd.Wait()
		msg := "exited"
		if err != nil {
			msg = "exited: " + err.Error()
		}
		hub.Publish(Event{Kind: "status", Text: msg})
		close(s.done)
	}()
	return s, nil
}

func (s *Session) Alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Send injects a user message. It works mid-run: the agent picks it up at its
// next model step (see RESULTS.md).
func (s *Session) Send(text string) error {
	s.hub.Publish(Event{Kind: "user", Text: text})
	s.hub.Publish(Event{Kind: "status", Text: "working"})
	msg := map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": []map[string]string{{"type": "text", "text": text}}},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.stdin.Write(append(b, '\n'))
	return err
}

// Cancel interrupts the agent process. There is no cooperative pause here.
func (s *Session) Cancel() {
	_ = s.cmd.Process.Signal(os.Interrupt)
}

func (s *Session) pumpErr(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			s.hub.Publish(Event{Kind: "error", Text: line})
		}
	}
}

func (s *Session) pump(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20)
	for sc.Scan() {
		s.normalize(sc.Bytes())
	}
}

type rawEvent struct {
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype"`
	SessionID    string          `json:"session_id"`
	Model        string          `json:"model"`
	APIKeySource string          `json:"apiKeySource"`
	CWD          string          `json:"cwd"`
	ToolName     string          `json:"tool_name"`
	Message      json.RawMessage `json:"message"` // an object for assistant/user, a string elsewhere
	Result       string          `json:"result"`
	IsError      bool            `json:"is_error"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	NumTurns     int             `json:"num_turns"`
	RateLimit    json.RawMessage `json:"rate_limit_info"`
}

type contentItem struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

func (s *Session) normalize(line []byte) {
	var ev rawEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		s.hub.Publish(Event{Kind: "error", Text: "unparsable output: " + truncate(string(line), 200)})
		return
	}
	switch ev.Type {
	case "system":
		switch ev.Subtype {
		case "init":
			d, _ := json.Marshal(map[string]string{
				"session_id": ev.SessionID, "model": ev.Model, "auth": ev.APIKeySource, "cwd": ev.CWD,
			})
			s.hub.Publish(Event{Kind: "session", Text: ev.Model, Data: d})
		case "permission_denied":
			// The agent wanted a tool that is not allowed. This is the hook for a
			// remote approval Decision (design 4.2).
			s.hub.Publish(Event{Kind: "permission", Tool: ev.ToolName, Text: "approval needed"})
		}
	case "assistant", "user":
		var msg struct {
			Content json.RawMessage `json:"content"`
		}
		var items []contentItem
		if json.Unmarshal(ev.Message, &msg) != nil || json.Unmarshal(msg.Content, &items) != nil {
			return
		}
		for _, it := range items {
			switch it.Type {
			case "text":
				if ev.Type == "assistant" {
					s.hub.Publish(Event{Kind: "text", Text: it.Text})
				}
			case "thinking":
				s.hub.Publish(Event{Kind: "thinking"})
			case "tool_use":
				s.hub.Publish(Event{Kind: "tool_call", Tool: it.Name, Data: it.Input})
			case "tool_result":
				kind := "tool_result"
				if it.IsError {
					kind = "error"
				}
				s.hub.Publish(Event{Kind: kind, Text: truncate(flatten(it.Content), 2000)})
			}
		}
	case "rate_limit_event":
		s.hub.Publish(Event{Kind: "usage", Data: ev.RateLimit})
	case "result":
		d, _ := json.Marshal(map[string]any{"cost_usd": ev.TotalCostUSD, "turns": ev.NumTurns, "is_error": ev.IsError, "subtype": ev.Subtype})
		s.hub.Publish(Event{Kind: "result", Text: ev.Result, Data: d})
		s.hub.Publish(Event{Kind: "status", Text: "idle"})
	}
}

// flatten renders tool-result content, which is a string or a list of blocks.
func flatten(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			sb.WriteString(b.Text)
		}
		return sb.String()
	}
	return fmt.Sprint(string(raw))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
