// Command transcript is a throwaway spike, not shipping code. It drives a
// coding agent headless, normalizes its output into typed events, streams them
// to a phone-sized web page over SSE, and injects messages mid-run.
//
// It runs the agent on the host (no isolation), so only point it at a scratch
// repository. See README.md.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

//go:embed index.html
var assets embed.FS

// Event is the normalized, agent-independent unit of the transcript.
type Event struct {
	ID   int             `json:"id"`
	Time time.Time       `json:"time"`
	Kind string          `json:"kind"` // session, user, text, thinking, tool_call, tool_result, usage, result, status, error
	Text string          `json:"text,omitempty"`
	Tool string          `json:"tool,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Hub is an append-only event log with broadcast, persisted as JSONL so a
// reload or a supervisor restart replays the whole transcript.
type Hub struct {
	mu        sync.Mutex
	events    []Event
	changed   chan struct{}
	file      *os.File
	status    string
	sessionID string
}

func newHub(dir string) (*Hub, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "events.jsonl")
	h := &Hub{changed: make(chan struct{}), status: "none"}
	if b, err := os.ReadFile(path); err == nil { //nolint:gosec // spike
		for _, line := range splitLines(b) {
			var e Event
			if json.Unmarshal(line, &e) == nil {
				h.events = append(h.events, e)
				h.apply(e)
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // spike
	if err != nil {
		return nil, err
	}
	h.file = f
	return h, nil
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// apply updates derived state from an event. Callers hold mu or own the hub.
func (h *Hub) apply(e Event) {
	switch e.Kind {
	case "status":
		h.status = e.Text
	case "session":
		var d struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.SessionID != "" {
			h.sessionID = d.SessionID
		}
	}
}

// Publish appends an event, persists it and wakes every SSE reader.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e.ID = len(h.events) + 1
	e.Time = time.Now().UTC()
	h.events = append(h.events, e)
	h.apply(e)
	if b, err := json.Marshal(e); err == nil {
		_, _ = h.file.Write(append(b, '\n'))
	}
	close(h.changed)
	h.changed = make(chan struct{})
}

// After returns the events with ID greater than last and a channel that is
// closed when more arrive.
func (h *Hub) After(last int) ([]Event, <-chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if last < 0 || last > len(h.events) {
		last = 0
	}
	return append([]Event(nil), h.events[last:]...), h.changed
}

func (h *Hub) State() (status, sessionID string, n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status, h.sessionID, len(h.events)
}

type server struct {
	hub       *Hub
	agent     Claude
	approvals *Approvals
	mu        sync.Mutex
	cur       *Session
	mode      string
}

// modes are the permission modes the page may choose. bypassPermissions is
// deliberately absent: it switches every prompt off, and this spike runs the
// agent on the host without isolation (design 6, default deny).
var modes = map[string]string{
	"manual":      "ask for everything not allowed",
	"acceptEdits": "auto-accept file edits, ask for the rest",
	"auto":        "agent decides what is safe, asks otherwise (behaviour untested)",
	"plan":        "read-only planning, no changes",
	"dontAsk":     "never ask: deny anything not allowed",
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	last := 0
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		last, _ = strconv.Atoi(v)
	} else if v := r.URL.Query().Get("since"); v != "" {
		last, _ = strconv.Atoi(v)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	for {
		evs, wait := s.hub.After(last)
		for _, e := range evs {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.ID, b)
			last = e.ID
		}
		fl.Flush()
		select {
		case <-wait:
		case <-r.Context().Done():
			return
		case <-time.After(15 * time.Second):
			fmt.Fprint(w, ": keepalive\n\n")
		}
	}
}

func (s *server) handleState(w http.ResponseWriter, _ *http.Request) {
	status, sid, n := s.hub.State()
	s.mu.Lock()
	live := s.cur != nil && s.cur.Alive()
	s.mu.Unlock()
	s.mu.Lock()
	mode := s.mode
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status, "session_id": sid, "events": n, "process_alive": live, "mode": mode, "modes": modes,
	})
}

func (s *server) text(r *http.Request) (string, bool) {
	var body struct {
		Text string `json:"text"`
	}
	if json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&body) != nil || body.Text == "" {
		return "", false
	}
	return body.Text, true
}

// handleStart starts a new session, or resumes the persisted one when resume=1.
func (s *server) handleStart(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil && s.cur.Alive() {
		http.Error(w, "a session is already running", http.StatusConflict)
		return
	}
	resume := ""
	if r.URL.Query().Get("resume") == "1" {
		_, resume, _ = s.hub.State()
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = s.mode
	}
	if mode == "" {
		mode = "manual"
	}
	if _, ok := modes[mode]; !ok {
		http.Error(w, "unknown or forbidden permission mode", http.StatusBadRequest)
		return
	}
	agent := s.agent
	agent.Mode = mode
	sess, err := agent.Start(s.hub, resume)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cur = sess
	s.mode = mode
	s.hub.Publish(Event{Kind: "mode", Text: mode})
	if text, ok := s.text(r); ok {
		if err := sess.Send(text); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) handleSay(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess := s.cur
	s.mu.Unlock()
	text, ok := s.text(r)
	if sess == nil || !sess.Alive() || !ok {
		http.Error(w, "no running session or empty text", http.StatusConflict)
		return
	}
	if err := sess.Send(text); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	sess := s.cur
	s.mu.Unlock()
	if sess == nil || !sess.Alive() {
		http.Error(w, "no running session", http.StatusConflict)
		return
	}
	sess.Cancel()
	w.WriteHeader(http.StatusAccepted)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "listen address (keep it on loopback)")
	dir := flag.String("dir", "", "working directory for the agent: a scratch repository, required")
	data := flag.String("data", "data", "directory for the persisted transcript")
	model := flag.String("model", "haiku", "model alias passed to the agent")
	tools := flag.String("tools", "Read,Bash(sleep:*),Bash(ls:*),Bash(cat:*)", "allowed tools; anything else is denied")
	bin := flag.String("claude", "claude", "claude binary")
	approve := flag.Bool("approvals", true, "route permission prompts to the page through an MCP approve tool")
	mcp := flag.Bool("mcp-permission", false, "internal: run as the MCP approve server the agent starts")
	supervisor := flag.String("supervisor", "http://127.0.0.1:8787", "internal: supervisor URL for -mcp-permission")
	flag.Parse()
	if *mcp {
		if err := runMCPPermission(*supervisor, os.Getenv("WH_TOKEN")); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *dir == "" {
		log.Fatal("-dir is required: point it at a scratch repository, never a real one")
	}
	hub, err := newHub(*data)
	if err != nil {
		log.Fatal(err)
	}
	s := &server{hub: hub, approvals: newApprovals(), agent: Claude{Bin: *bin, Dir: *dir, Model: *model, Tools: *tools}}
	if *approve {
		cfg, err := writeMCPConfig(*data, "http://"+*addr, s.approvals.token)
		if err != nil {
			log.Fatal(err)
		}
		s.agent.MCPConfig = cfg
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /approve", s.handleApprove)
	mux.HandleFunc("POST /internal/permission", s.handlePermission)
	mux.Handle("GET /", http.FileServerFS(assets))
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /state", s.handleState)
	mux.HandleFunc("POST /start", s.handleStart)
	mux.HandleFunc("POST /say", s.handleSay)
	mux.HandleFunc("POST /cancel", s.handleCancel)
	log.Printf("transcript spike on http://%s (agent dir %s)", *addr, *dir)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
