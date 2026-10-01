package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// approvalTimeout bounds how long the agent waits for a human. After it, the
// request is denied, so a detached run never hangs forever (design 5.2).
const approvalTimeout = 10 * time.Minute

type decision struct {
	Allow   bool
	Message string
}

type pendingApproval struct {
	id      string
	decided chan decision
}

// Approvals holds permission requests that the agent has raised and a human
// has not answered yet. It is the spike's version of a blocking Decision.
type Approvals struct {
	mu      sync.Mutex
	seq     int
	pending map[string]*pendingApproval
	token   string
}

func newApprovals() *Approvals {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &Approvals{pending: map[string]*pendingApproval{}, token: hex.EncodeToString(b)}
}

func (a *Approvals) open() *pendingApproval {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	p := &pendingApproval{id: "a-" + strconv.Itoa(a.seq), decided: make(chan decision, 1)}
	a.pending[p.id] = p
	return p
}

func (a *Approvals) close(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.pending, id)
}

func (a *Approvals) decide(id string, d decision) bool {
	a.mu.Lock()
	p := a.pending[id]
	a.mu.Unlock()
	if p == nil {
		return false
	}
	select {
	case p.decided <- d:
		return true
	default:
		return false
	}
}

// handlePermission is called by the MCP helper while the agent waits. It
// publishes an approval event, blocks until a human answers, and returns the
// decision in the shape the agent expects.
func (s *server) handlePermission(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Token")), []byte(s.approvals.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		ToolName  string          `json:"tool_name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := s.approvals.open()
	defer s.approvals.close(p.id)
	data, _ := json.Marshal(map[string]any{"id": p.id, "tool_use_id": req.ToolUseID, "input": capInput(req.Input, 2000)})
	s.hub.Publish(Event{Kind: "approval", Tool: req.ToolName, Text: "pending", Data: data})
	s.hub.Publish(Event{Kind: "status", Text: "awaiting approval"})

	var d decision
	select {
	case d = <-p.decided:
	case <-time.After(approvalTimeout):
		d = decision{Message: "no answer within " + approvalTimeout.String()}
	case <-r.Context().Done():
		d = decision{Message: "supervisor request cancelled"}
	}
	verdict := "denied"
	if d.Allow {
		verdict = "allowed"
	}
	rd, _ := json.Marshal(map[string]string{"id": p.id, "message": d.Message})
	s.hub.Publish(Event{Kind: "approval_result", Tool: req.ToolName, Text: verdict, Data: rd})
	s.hub.Publish(Event{Kind: "status", Text: "working"})

	resp := map[string]any{"behavior": "deny", "message": d.Message}
	if d.Allow {
		resp = map[string]any{"behavior": "allow", "updatedInput": req.Input}
	} else if d.Message == "" {
		resp["message"] = "The user denied this action."
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleApprove is the human side, called from the page.
func (s *server) handleApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Allow   bool   `json:"allow"`
		Message string `json:"message"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) != nil || body.ID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.approvals.decide(body.ID, decision{Allow: body.Allow, Message: body.Message}) {
		http.Error(w, "no such pending approval (already answered, timed out, or the supervisor restarted)", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// capInput keeps large tool inputs, such as a whole file being written, from
// flooding every SSE message and the log. The full input stays with the agent.
func capInput(raw json.RawMessage, n int) json.RawMessage {
	if len(raw) <= n {
		return raw
	}
	b, _ := json.Marshal(map[string]any{"truncated": true, "bytes": len(raw), "preview": truncate(string(raw), n)})
	return b
}
