// Package claude is the Claude Code agent adapter (design §5.2). It drives the
// CLI headless over stream-json through the runtime's Exec and turns its
// output into the contract's typed events.
package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// maxLine is the longest output line the parser keeps. A Write carries a whole
// file in one line, so it is generous; a longer line is dropped with an error
// event instead of growing without bound.
const maxLine = 32 << 20

// Stream shapes, from spike #1 (spikes/transcript/RESULTS.md). Only the fields
// the adapter reads are listed.
type rawEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Model     string          `json:"model"`
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	Message   json.RawMessage `json:"message"` // an object for assistant and user events
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	TotalCost *float64        `json:"total_cost_usd"`
	// DurationMS and DurationAPIMS are on the result event (spike #1): the turn's wall
	// time and the part of it spent waiting on the model API.
	DurationMS    int64           `json:"duration_ms"`
	DurationAPIMS int64           `json:"duration_api_ms"`
	RateLimit     json.RawMessage `json:"rate_limit_info"`
	// Usage is on the result event (recorded in spike #7): the tokens of the run.
	Usage *struct {
		InputTokens      int64 `json:"input_tokens"`
		OutputTokens     int64 `json:"output_tokens"`
		CacheReadTokens  int64 `json:"cache_read_input_tokens"`
		CacheWriteTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	// ToolResultMeta says, for a tool result on a user event, when the tool was
	// not executed at all: "non_execution_kind" is "permission-rule" for a denial.
	ToolResultMeta []struct {
		ID               string `json:"id"`
		NonExecutionKind string `json:"non_execution_kind"`
	} `json:"tool_result_meta"`
	Error json.RawMessage `json:"error"` // a short code on assistant events
}

type contentItem struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
	UseID   string          `json:"tool_use_id"`
}

type rateLimit struct {
	Status  string `json:"status"` // "allowed" in the measured streams; the exhausted value was not observed
	Windows map[string]struct {
		Utilization float64         `json:"utilization"`
		ResetsAt    json.RawMessage `json:"resetsAt"`
	} `json:"unifiedWindows"`
}

// parser turns stream-json lines into events and keeps what the final result
// needs. It is used from one goroutine.
type parser struct {
	now       func() time.Time
	dontAsk   bool            // record each tool decision as an approval event
	allowlist map[string]bool // tools that run without asking in dontAsk mode

	buf     bytes.Buffer
	dropped bool // inside a line that was too long

	sessionID string
	model     string
	windows   []agent.UsageWindow
	pending   map[string]string // tool_use ID to tool name, until its outcome is known
	order     []string          // pending IDs, oldest first

	controls   []controlRequest // control_request lines seen, for the session to answer
	denials    int              // denials that matched no tool use, for unique IDs
	sawInit    bool
	authFailed bool
	exhausted  bool
	resetAt    time.Time
	result     *rawEvent // the result event, once seen
}

func newParser(now func() time.Time, dontAsk bool, allowed []string) *parser {
	p := &parser{now: now, dontAsk: dontAsk, allowlist: map[string]bool{}, pending: map[string]string{}}
	for _, t := range allowed {
		p.allowlist[t] = true
	}
	return p
}

// feed adds output and returns the events of every line it completed. Output
// arrives in chunks that may end anywhere in a line.
func (p *parser) feed(data []byte) []agent.Event {
	var events []agent.Event
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			if p.dropped || p.buf.Len()+len(data) > maxLine {
				if !p.dropped {
					events = append(events, p.errorEvent("an output line is longer than the limit and was dropped"))
				}
				p.dropped = true
				p.buf.Reset()
				return events
			}
			p.buf.Write(data)
			return events
		}
		if p.dropped {
			p.dropped = false
		} else if p.buf.Len()+i > maxLine {
			events = append(events, p.errorEvent("an output line is longer than the limit and was dropped"))
		} else {
			p.buf.Write(data[:i])
			if line := bytes.TrimSpace(p.buf.Bytes()); len(line) > 0 {
				events = append(events, p.line(line)...)
			}
		}
		p.buf.Reset()
		data = data[i+1:]
	}
	return events
}

func (p *parser) event(kind agent.EventKind) agent.Event {
	return agent.Event{Kind: kind, At: p.now(), SessionID: p.sessionID}
}

func (p *parser) errorEvent(text string) agent.Event {
	e := p.event(agent.EventError)
	e.Text = capText(text)
	return e
}

func (p *parser) line(b []byte) []agent.Event {
	var ev rawEvent
	if err := json.Unmarshal(b, &ev); err != nil {
		return []agent.Event{p.errorEvent("unparsable output: " + capText(string(b)))}
	}
	if ev.SessionID != "" && p.sessionID == "" && ev.Type != "system" {
		// A result or message may carry the ID before, or without, an init.
		p.sessionID = ev.SessionID
	}
	switch ev.Type {
	case "system":
		return p.system(ev)
	case "assistant", "user":
		return p.message(ev)
	case "rate_limit_event":
		return p.rateLimit(ev)
	case "result":
		return p.finish(ev)
	case "control_request":
		return p.control(b)
	}
	return nil // stream_event deltas and anything new: not part of the contract yet
}

func (p *parser) system(ev rawEvent) []agent.Event {
	switch ev.Subtype {
	case "init":
		p.sawInit = true
		p.sessionID, p.model = ev.SessionID, ev.Model
		e := p.event(agent.EventSession)
		e.Text = ev.Model
		return []agent.Event{e}
	case "permission_denied":
		if !p.dontAsk {
			return nil // outside dontAsk the host answers prompts, and records them itself
		}
		id := p.pendingByName(ev.ToolUseID, ev.ToolName)
		if id == "" {
			p.denials++ // a denial with no tool use before it: the ID must still be its own
			id = fmt.Sprintf("denied-%d-%s", p.denials, ev.ToolName)
		}
		return []agent.Event{p.approval(id, ev.ToolName, false, "not allowed in dontAsk mode: denied")}
	}
	return nil
}

// pendingByName takes the pending tool use a denial is about: by its ID when
// the event has one, else the oldest pending use of that tool.
func (p *parser) pendingByName(id, name string) string {
	if _, ok := p.pending[id]; ok && id != "" {
		p.dropPending(id)
		return id
	}
	for _, pid := range p.order {
		if p.pending[pid] == name {
			p.dropPending(pid)
			return pid
		}
	}
	return ""
}

func (p *parser) dropPending(id string) {
	delete(p.pending, id)
	for i, o := range p.order {
		if o == id {
			p.order = append(p.order[:i], p.order[i+1:]...)
			return
		}
	}
}

// toolDecision records what happened to a tool use that no denial event covered. A
// tool on the allowlist was permitted whatever its result. For any other tool
// a result that is not an error means the agent's own rules let it run; a
// result that is an error is how a denial can come back, so it is never
// recorded as allowed: the audit trail must not say "allowed" for a tool that
// may have been refused.
func (p *parser) toolDecision(id, tool string, isError bool, nonExec string) agent.Event {
	switch {
	case nonExec != "":
		// The CLI says the tool never ran (recorded: "permission-rule" for a
		// denial), whatever the allowlist says.
		return p.approval(id, tool, false, "not executed ("+nonExec+"): denied")
	case p.allowlist[tool]:
		return p.approval(id, tool, true, "on the allowlist")
	case isError:
		return p.approval(id, tool, false, "the tool returned an error and no denial was seen: recorded as denied")
	default:
		return p.approval(id, tool, true, "allowed by the agent's own rules")
	}
}

func (p *parser) approval(id, tool string, allow bool, reason string) agent.Event {
	e := p.event(agent.EventApproval)
	e.Tool = tool
	e.Approval = &agent.ApprovalRecord{ID: id, Allow: allow, Reason: reason}
	return e
}

func (p *parser) message(ev rawEvent) []agent.Event {
	var events []agent.Event
	if code := errorCode(ev.Error); code != "" && ev.Type == "assistant" {
		if code == "authentication_failed" {
			p.authFailed = true
			e := p.event(agent.EventAuthExpired)
			e.Text = code
			events = append(events, e)
		} else {
			events = append(events, p.errorEvent("agent error: "+code))
		}
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	var items []contentItem
	if json.Unmarshal(ev.Message, &msg) != nil || json.Unmarshal(msg.Content, &items) != nil {
		return events // content may be a plain string, which carries nothing the contract needs
	}
	for _, it := range items {
		switch it.Type {
		case "text":
			if ev.Type == "assistant" {
				e := p.event(agent.EventMessage)
				e.Text = capText(it.Text)
				events = append(events, e)
			}
		case "tool_use":
			e := p.event(agent.EventToolCall)
			e.Tool, e.Input = it.Name, capText(compact(it.Input))
			events = append(events, e)
			if p.dontAsk && it.ID != "" {
				p.pending[it.ID] = it.Name
				p.order = append(p.order, it.ID)
			}
		case "tool_result":
			if tool, ok := p.pending[it.UseID]; ok {
				nonExec := ""
				for _, m := range ev.ToolResultMeta {
					if m.ID == it.UseID {
						nonExec = m.NonExecutionKind
					}
				}
				events = append(events, p.toolDecision(it.UseID, tool, it.IsError, nonExec))
				p.dropPending(it.UseID)
			}
			e := p.event(agent.EventToolResult)
			e.Text = capText(flatten(it.Content))
			events = append(events, e)
		}
	}
	return events
}

func (p *parser) rateLimit(ev rawEvent) []agent.Event {
	var rl rateLimit
	if json.Unmarshal(ev.RateLimit, &rl) != nil {
		return nil
	}
	p.windows = p.windows[:0]
	var events []agent.Event
	// A window at full utilization is exhausted unless the stream says the
	// limit still allows the request. Unverified: the value of status when the
	// limit is reached was never observed, so only "allowed" is trusted.
	allowed := rl.Status == "allowed"
	for _, name := range []string{agent.WindowFiveHour, agent.WindowSevenDay} {
		w, ok := rl.Windows[name]
		if !ok {
			continue
		}
		reset := parseTime(w.ResetsAt)
		p.windows = append(p.windows, agent.UsageWindow{Name: name, Utilization: clamp01(w.Utilization), ResetsAt: reset})
		// Unverified: what the stream reads when a window is used up was not
		// observed, so a window at full utilization is taken as exhausted.
		if w.Utilization >= 1 && !allowed && !p.exhausted {
			p.exhausted, p.resetAt = true, reset
			e := p.event(agent.EventQuotaExhausted)
			e.ResetAt = reset
			events = append(events, e)
		}
	}
	return events
}

// finish handles the result event: one usage event, then the run is over.
func (p *parser) finish(ev rawEvent) []agent.Event {
	p.result = &ev
	if ev.SessionID != "" {
		p.sessionID = ev.SessionID
	}
	if p.model == "" || p.authFailed {
		return nil // nothing to report usage on
	}
	u := agent.Usage{Model: p.model, Windows: append([]agent.UsageWindow(nil), p.windows...)}
	if ev.TotalCost != nil && *ev.TotalCost >= 0 {
		u.Cost = &agent.Cost{MicroUSD: int64(math.Round(*ev.TotalCost * 1e6)), Source: agent.CostReported}
	}
	if t := ev.Usage; t != nil && t.InputTokens >= 0 && t.OutputTokens >= 0 && t.CacheReadTokens >= 0 && t.CacheWriteTokens >= 0 {
		u.Tokens = &agent.TokenCounts{Input: t.InputTokens, Output: t.OutputTokens, CacheRead: t.CacheReadTokens, CacheWrite: t.CacheWriteTokens}
	}
	if ev.DurationMS > 0 {
		u.WallMillis = ev.DurationMS
	}
	if ev.DurationAPIMS > 0 {
		u.APIMillis = ev.DurationAPIMS
	}
	e := p.event(agent.EventUsage)
	e.Usage = &u
	return []agent.Event{e}
}

// outcome is the session's result once the process has ended. A stop is
// decided by the caller, not here.
func (p *parser) outcome() (agent.Result, bool) {
	if p.result == nil {
		return agent.Result{}, false
	}
	r := agent.Result{SessionID: p.sessionID, Text: capText(p.result.Result)}
	switch {
	case p.authFailed:
		// The result says is_error with subtype "success": the error code on
		// the assistant event is what counts (spike #1).
		r.Status = agent.ResultAuthExpired
	case p.exhausted:
		r.Status, r.ResetAt = agent.ResultQuotaExhausted, p.resetAt
	case p.result.IsError:
		r.Status = agent.ResultFailed
	default:
		r.Status = agent.ResultCompleted
	}
	return r, true
}

func errorCode(raw json.RawMessage) string {
	var code string
	if json.Unmarshal(raw, &code) == nil {
		return code
	}
	return ""
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
	return string(raw)
}

func compact(raw json.RawMessage) string {
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		return string(raw)
	}
	return out.String()
}

// capText keeps at most domain.MaxDecisionInput characters of text that comes
// from the agent or the repository, which is untrusted.
func capText(s string) string {
	if utf8.RuneCountInString(s) <= domain.MaxDecisionInput {
		return s
	}
	return string([]rune(s)[:domain.MaxDecisionInput])
}

// parseTime reads a reset time given as epoch seconds or as RFC 3339. Which of
// the two the CLI uses is unverified. A value it cannot read is the zero time.
func parseTime(raw json.RawMessage) time.Time {
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		if n > 1e12 { // milliseconds
			n /= 1000
		}
		return time.Unix(int64(n), 0).UTC()
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func clamp01(f float64) float64 {
	return math.Min(1, math.Max(0, f))
}

// controlRequest is a request of the CLI to the host on the stdio control
// channel (design D26). Only can_use_tool, a permission prompt, is understood;
// the shape is from spike #7 (spikes/agent-approval/results).
type controlRequest struct {
	ID      string
	Subtype string
	Tool    string
	Input   json.RawMessage
	UseID   string
}

// control records a control_request for the session to answer. A request with
// no ID cannot be answered, so it is reported and dropped.
func (p *parser) control(b []byte) []agent.Event {
	var raw struct {
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype  string          `json:"subtype"`
			ToolName string          `json:"tool_name"`
			Input    json.RawMessage `json:"input"`
			ToolUse  string          `json:"tool_use_id"`
		} `json:"request"`
	}
	if json.Unmarshal(b, &raw) != nil || raw.RequestID == "" {
		return []agent.Event{p.errorEvent("a control request without an ID was dropped")}
	}
	p.controls = append(p.controls, controlRequest{ID: raw.RequestID, Subtype: raw.Request.Subtype, Tool: raw.Request.ToolName, Input: raw.Request.Input, UseID: raw.Request.ToolUse})
	return nil
}

// takeControls returns the control requests seen since the last call.
func (p *parser) takeControls() []controlRequest {
	c := p.controls
	p.controls = nil
	return c
}

// approvalInput is what the human is shown of a permission prompt: a Bash
// command, a plan, or else the tool's input as compact JSON. It is untrusted
// and capped by the caller.
func approvalInput(tool string, raw json.RawMessage) (text string, plan bool) {
	var in map[string]any
	if json.Unmarshal(raw, &in) == nil {
		switch tool {
		case "Bash":
			if c, ok := in["command"].(string); ok {
				return c, false
			}
		case "ExitPlanMode":
			if pl, ok := in["plan"].(string); ok {
				return pl, true
			}
		}
	}
	return compact(raw), tool == "ExitPlanMode"
}
