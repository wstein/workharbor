package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/runtime"
)

// replay plays a recorded stream as the CLI would: it waits for the first user
// message, then writes the recorded lines, and at each control_request waits
// for the control_response the adapter writes before it goes on. With
// closeStdin it closes the adapter's input instead, as a lost channel does.
type replay struct {
	lines      []string
	closeStdin bool

	mu  sync.Mutex
	got []map[string]any // what the adapter wrote on stdin, control responses only
}

func (r *replay) responses() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.got)
}

func (r *replay) Exec(ctx context.Context, _ string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if st, ok := preflight(req, ""); ok {
		return st, nil
	}
	st := &stubStream{chunks: make(chan runtime.Chunk, 16), done: make(chan struct{})}
	in := make(chan map[string]any, 8)
	go func() {
		defer close(in)
		sc := bufio.NewScanner(req.Stdin)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			if m["type"] == "control_response" {
				r.mu.Lock()
				r.got = append(r.got, m)
				r.mu.Unlock()
			}
			in <- m
		}
	}()
	go func() {
		defer close(st.done)
		defer close(st.chunks)
		select { // the CLI says nothing before its first message
		case <-in:
		case <-ctx.Done():
			st.code, st.err = 130, ctx.Err()
			return
		}
		for _, l := range r.lines {
			select {
			case st.chunks <- runtime.Chunk{Stream: runtime.Stdout, Data: []byte(l + "\n")}:
			case <-ctx.Done():
				st.code, st.err = 130, ctx.Err()
				return
			}
			if !strings.Contains(l, `"type":"control_request"`) {
				continue
			}
			if r.closeStdin {
				if pr, ok := req.Stdin.(*io.PipeReader); ok {
					_ = pr.CloseWithError(errors.New("the channel is gone"))
				}
				<-ctx.Done() // the CLI waits; only the supervisor's stop ends it
				st.code, st.err = 130, ctx.Err()
				return
			}
			select {
			case <-in:
			case <-ctx.Done():
				st.code, st.err = 130, ctx.Err()
				return
			case <-time.After(5 * time.Second):
				st.code, st.err = 1, errors.New("no answer to the control request")
				return
			}
		}
	}()
	return st, nil
}

func recordedLines(t *testing.T, name string) []string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(string(recorded(t, name)), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

type approvalRun struct {
	events []agent.Event
	result agent.Result
	asked  []agent.ApprovalRequest
	resp   []map[string]any
}

// runRecorded starts a manual-mode session over a recorded stream and collects
// everything. The approver gives answer for every request.
func runRecorded(t *testing.T, name string, closeStdin bool, answer agent.Approval) approvalRun {
	t.Helper()
	rp := &replay{lines: recordedLines(t, name), closeStdin: closeStdin}
	ad := New(rp, Config{})
	ad.now = clock
	var out approvalRun
	var mu sync.Mutex
	spec := agent.StartSpec{
		EnvID: "env-1", Workdir: "/work", Prompt: "do it", Auth: agent.AuthSubscription, PermissionMode: agent.PermissionManual,
		ApprovalTimeout: 5 * time.Second,
		Approver: agent.ApproverFunc(func(_ context.Context, req agent.ApprovalRequest) (agent.Approval, error) {
			mu.Lock()
			out.asked = append(out.asked, req)
			mu.Unlock()
			return answer, nil
		}),
	}
	s, err := ad.Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range s.Events() {
			out.events = append(out.events, e)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	out.result, _ = s.Wait()
	out.resp = rp.responses()
	return out
}

func approvals(events []agent.Event) []agent.ApprovalRecord {
	var out []agent.ApprovalRecord
	for _, e := range events {
		if e.Kind == agent.EventApproval {
			out = append(out, *e.Approval)
		}
	}
	return out
}

// The recorded streams of spike #7 drive a real session: the control_request
// becomes a prompt for the approver, and the answer goes back as a
// control_response with the request_id the CLI gave.
func TestRecordedApprovalsGoBackByRequestID(t *testing.T) {
	cases := []struct {
		name   string
		answer agent.Approval
		id     string
		tool   string
	}{
		{"case1-allow", agent.Approval{Allow: true}, "8d44de59-8a20-4a04-a9ab-77a163304189", "Write"},
		{"case2-deny", agent.Approval{Reason: "Denied by supervisor policy"}, "270104dc-5bcf-4d5c-a215-fd0f8c669d29", "Write"},
		{"case6-reqid", agent.Approval{Reason: "Denied after bad request_id test"}, "decb182c-6bcc-412b-b70d-581d3581636d", "Write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runRecorded(t, tc.name, false, tc.answer)
			if len(run.asked) != 1 || run.asked[0].ID != tc.id || run.asked[0].Tool != tc.tool {
				t.Fatalf("approver asked %+v, want one %s request %s", run.asked, tc.tool, tc.id)
			}
			if len(run.resp) != 1 {
				t.Fatalf("%d control responses, want one: %v", len(run.resp), run.resp)
			}
			resp, _ := run.resp[0]["response"].(map[string]any)
			if resp["request_id"] != tc.id || resp["subtype"] != "success" {
				t.Errorf("response %v is not a success for request %s", resp, tc.id)
			}
			recs := approvals(run.events)
			if len(recs) != 1 || recs[0].ID != tc.id || recs[0].Allow != tc.answer.Allow {
				t.Errorf("approval records %+v", recs)
			}
			if run.result.Status != agent.ResultCompleted {
				t.Errorf("result %+v, want completed", run.result)
			}
			golden(t, filepath.Join("recorded", tc.name+".session"), map[string]any{"asked": run.asked, "responses": run.resp, "events": run.events, "result": run.result})
		})
	}
}

// A closed channel while a request is open (spike #7, case 3): the allow cannot
// be delivered, so it is recorded as a denial, and the agent is stopped, not
// left waiting.
func TestALostChannelDeniesAndStopsTheAgent(t *testing.T) {
	run := runRecorded(t, "case3-closed-stdin", true, agent.Approval{Allow: true})
	recs := approvals(run.events)
	if len(recs) != 1 || recs[0].Allow || !strings.Contains(recs[0].Reason, "lost") {
		t.Fatalf("approval records %+v, want one denial that says the channel was lost", recs)
	}
	if run.result.Status != agent.ResultStopped {
		t.Errorf("result %+v, want stopped", run.result)
	}
	golden(t, filepath.Join("recorded", "case3-closed-stdin.session"), map[string]any{"asked": run.asked, "events": run.events, "result": run.result})
}

func TestManualModeCommandLine(t *testing.T) {
	h, st := harness(t)
	spec := h.Spec()
	spec.Approver = approveEverything()
	s, err := h.Adapter.Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	for range s.Events() {
	}
	cmd := st.lastCall()
	if argAfter(cmd, "--permission-mode") != "manual" || argAfter(cmd, "--permission-prompt-tool") != "stdio" || argAfter(cmd, "--permission-prompts") != "host" {
		t.Errorf("command %v does not ask the host", cmd)
	}
	if slices.Contains(cmd, "--allowedTools") || strings.Contains(strings.Join(cmd, " "), "dontAsk") || slices.Contains(cmd, "none") {
		t.Errorf("command %v carries dontAsk's settings", cmd)
	}
	for _, flag := range []string{"--setting-sources", "--strict-mcp-config", "--disable-slash-commands"} {
		if !slices.Contains(cmd, flag) {
			t.Errorf("command %v lacks %s: the CLI must read only what the supervisor passes", cmd, flag)
		}
	}
}

func approveEverything() agent.Approver {
	return agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) {
		return agent.Approval{Allow: true}, nil
	})
}

// raw drives the adapter with lines of our own.
type raw struct {
	lines []string
	mu    sync.Mutex
	in    []string
}

func (r *raw) Exec(ctx context.Context, _ string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if st, ok := preflight(req, ""); ok {
		return st, nil
	}
	st := &stubStream{chunks: make(chan runtime.Chunk, 16), done: make(chan struct{})}
	first := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(req.Stdin)
		var once sync.Once
		for sc.Scan() {
			r.mu.Lock()
			r.in = append(r.in, sc.Text())
			r.mu.Unlock()
			once.Do(func() { close(first) })
		}
	}()
	go func() {
		defer close(st.done)
		defer close(st.chunks)
		select {
		case <-first:
		case <-ctx.Done():
			return
		}
		for _, l := range r.lines {
			select {
			case st.chunks <- runtime.Chunk{Stream: runtime.Stdout, Data: []byte(l + "\n")}:
			case <-ctx.Done():
				return
			}
		}
		<-ctx.Done()
		st.code, st.err = 130, ctx.Err()
	}()
	return st, nil
}

func (r *raw) written() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.in)
}

const initLine = `{"type":"system","subtype":"init","session_id":"s1","model":"claude-haiku-4-5"}`

func ctlRequest(id, subtype, tool, input string) string {
	return `{"type":"control_request","request_id":"` + id + `","request":{"subtype":"` + subtype + `","tool_name":"` + tool + `","input":` + input + `}}`
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 300 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A request that is not a permission prompt is refused at once, and a second
// request with an open ID is not asked: it is denied. Neither leaves the CLI
// waiting.
func TestOnlyPermissionPromptsAreAskedAndAnIDIsOpenOnce(t *testing.T) {
	r := &raw{lines: []string{
		initLine,
		ctlRequest("a", "hook_callback", "", `{}`),
		ctlRequest("b", "can_use_tool", "Bash", `{"command":"ls"}`),
		ctlRequest("b", "can_use_tool", "Bash", `{"command":"rm -rf /"}`),
	}}
	ad := New(r, Config{})
	release := make(chan struct{})
	var mu sync.Mutex
	var asked []string
	spec := agent.StartSpec{
		EnvID: "e", Workdir: "/work", Prompt: "go", Auth: agent.AuthSubscription, PermissionMode: agent.PermissionManual, ApprovalTimeout: 5 * time.Second,
		Approver: agent.ApproverFunc(func(_ context.Context, req agent.ApprovalRequest) (agent.Approval, error) {
			mu.Lock()
			asked = append(asked, req.Input)
			mu.Unlock()
			<-release
			return agent.Approval{Allow: true}, nil
		}),
	}
	s, err := ad.Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range s.Events() {
		}
	}()
	waitUntil(t, "two refusals", func() bool { return len(r.written()) >= 3 })
	close(release)
	waitUntil(t, "all answers", func() bool { return len(r.written()) >= 4 })
	_ = s.Stop(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != "ls" {
		t.Errorf("the approver was asked %q, want only the first can_use_tool request", asked)
	}
	var got []string
	for _, l := range r.written()[1:] { // [0] is the user message
		var m struct {
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Response  struct {
					Behavior string `json:"behavior"`
				} `json:"response"`
			} `json:"response"`
		}
		_ = json.Unmarshal([]byte(l), &m)
		got = append(got, m.Response.RequestID+":"+m.Response.Subtype+":"+m.Response.Response.Behavior)
	}
	slices.Sort(got)
	want := []string{"a:error:", "b:success:allow", "b:success:deny"}
	if !slices.Equal(got, want) {
		t.Errorf("responses %v, want %v", got, want)
	}
}

// A prompt nobody answers in time is denied and recorded, and the agent is told.
func TestAnUnansweredPromptIsDeniedAfterItsDeadline(t *testing.T) {
	r := &raw{lines: []string{initLine, ctlRequest("slow", "can_use_tool", "Bash", `{"command":"make deploy"}`)}}
	ad := New(r, Config{})
	spec := agent.StartSpec{
		EnvID: "e", Workdir: "/work", Prompt: "go", Auth: agent.AuthSubscription, PermissionMode: agent.PermissionManual, ApprovalTimeout: 50 * time.Millisecond,
		Approver: agent.ApproverFunc(func(ctx context.Context, _ agent.ApprovalRequest) (agent.Approval, error) {
			<-ctx.Done()
			return agent.Approval{Allow: true}, nil
		}),
	}
	s, err := ad.Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	var events []agent.Event
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for e := range s.Events() {
			events = append(events, e)
		}
	}()
	waitUntil(t, "the denial", func() bool { return len(r.written()) >= 2 })
	if l := r.written()[1]; !strings.Contains(l, `"behavior":"deny"`) || !strings.Contains(l, `"request_id":"slow"`) || !strings.Contains(l, "no answer in time") {
		t.Errorf("the agent was told %s", l)
	}
	_ = s.Stop(context.Background())
	_, _ = s.Wait()
	// Wait returns once the events channel is closed, not once the collector
	// has appended the last event it received: wait for the collector.
	<-collected
	if recs := approvals(events); len(recs) != 1 || recs[0].Allow {
		t.Errorf("approval records %+v, want one denial", recs)
	}
}

func TestApprovalInputIsWhatTheHumanNeeds(t *testing.T) {
	for _, tc := range []struct {
		tool, raw, want string
		plan            bool
	}{
		{"Bash", `{"command":"ls -la","description":"x"}`, "ls -la", false},
		{"ExitPlanMode", `{"plan":"1. do it"}`, "1. do it", true},
		{"Write", `{"file_path":"/work/a","content":"c"}`, `{"file_path":"/work/a","content":"c"}`, false},
		{"Bash", `{"cmd":1}`, `{"cmd":1}`, false},
	} {
		got, plan := approvalInput(tc.tool, json.RawMessage(tc.raw))
		if got != tc.want || plan != tc.plan {
			t.Errorf("%s %s: %q plan=%v, want %q plan=%v", tc.tool, tc.raw, got, plan, tc.want, tc.plan)
		}
	}
}

// A tool name that is not one is denied without asking, and so is a prompt
// beyond maxOpenPrompts open at once: a flood never reaches the human's inbox.
func TestBadToolNamesAndAFloodAreDeniedWithoutAsking(t *testing.T) {
	lines := []string{initLine, ctlRequest("bad", "can_use_tool", strings.Repeat("x", 300), `{}`)}
	for i := range maxOpenPrompts + 2 {
		lines = append(lines, ctlRequest("p"+strconv.Itoa(i), "can_use_tool", "Bash", `{"command":"ls"}`))
	}
	r := &raw{lines: lines}
	release := make(chan struct{})
	var mu sync.Mutex
	asked := 0
	spec := agent.StartSpec{
		EnvID: "e", Workdir: "/work", Prompt: "go", Auth: agent.AuthSubscription, PermissionMode: agent.PermissionManual, ApprovalTimeout: 5 * time.Second,
		Approver: agent.ApproverFunc(func(context.Context, agent.ApprovalRequest) (agent.Approval, error) {
			mu.Lock()
			asked++
			mu.Unlock()
			<-release
			return agent.Approval{Allow: true}, nil
		}),
	}
	s, err := New(r, Config{}).Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	var recs []agent.ApprovalRecord
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for e := range s.Events() {
			if e.Approval != nil {
				recs = append(recs, *e.Approval)
			}
		}
	}()
	// The bad name and the two prompts beyond the limit are answered at once,
	// and every approver goroutine must have called in before the count is read.
	waitUntil(t, "three denials", func() bool { return len(r.written()) >= 4 })
	waitUntil(t, "every open prompt asked", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return asked >= maxOpenPrompts
	})
	// Give a wrongly admitted prompt beyond the limit time to show up.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := asked
	mu.Unlock()
	close(release)
	waitUntil(t, "all answers", func() bool { return len(r.written()) >= 1+1+maxOpenPrompts+2 })
	_ = s.Stop(context.Background())
	_, _ = s.Wait()
	<-collected // the events are closed; every record is in recs
	if n != maxOpenPrompts {
		t.Errorf("the human was asked %d times, want %d", n, maxOpenPrompts)
	}
	seen := false
	for _, rec := range recs {
		if rec.ID == "bad" {
			seen = true
			if rec.Allow || !strings.Contains(rec.Reason, "not a tool name") {
				t.Errorf("the bad tool name got %+v", rec)
			}
		}
	}
	if !seen {
		t.Errorf("no approval record for the bad tool name in %+v", recs)
	}
}
