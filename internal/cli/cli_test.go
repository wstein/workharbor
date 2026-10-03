package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/exitcode"
)

const tok = "tok-123"

// stub is an API server that answers canned envelopes and records requests.
type stub struct {
	t   *testing.T
	ts  *httptest.Server
	mu  sync.Mutex
	req []recorded
	// handlers by "METHOD path" (query string excluded)
	h map[string]func(w http.ResponseWriter, r *http.Request, body string)
}

type recorded struct {
	method, path, query, body string
	header                    http.Header
}

func ok(data string) string { return `{"schema_version":1,"ok":true,"data":` + data + `}` + "\n" }
func fail(code string, exit int, msg string) string {
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": code, "exit_code": exit, "message": msg}})
	return string(b) + "\n"
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{t: t, h: map[string]func(http.ResponseWriter, *http.Request, string){}}
	s.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.req = append(s.req, recorded{r.Method, r.URL.Path, r.URL.RawQuery, string(b), r.Header.Clone()})
		h := s.h[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if h == nil {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, fail("not_found", 3, "no such route in the stub"))
			return
		}
		h(w, r, string(b))
	}))
	t.Cleanup(s.ts.Close)
	return s
}

func (s *stub) reply(route string, status int, body string) {
	s.h[route] = func(w http.ResponseWriter, _ *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func (s *stub) requests(route string) []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []recorded
	for _, r := range s.req {
		if r.method+" "+r.path == route {
			out = append(out, r)
		}
	}
	return out
}

// safeBuf is a buffer a test may read while a command writes to it.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// runCLI executes whr against the stub with the given standard input.
func (s *stub) runCLI(stdin string, args ...string) (code int, stdout, stderr string) {
	s.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" },
		NewClient: func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil },
		// a host that reads nothing: no test looks at the machine it runs on
		Setup: SetupEnv{Host: &setupHost{outputs: map[string]string{}}, User: "whr", UID: 502, GOOS: "linux"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code = Execute(ctx, env, args)
	return code, out.String(), errOut.String()
}

const taskList = `[
	{"id":"t-aaa111","repo":"wstein/workharbor","issue":"#7","state":"running","agent_id":"a1"},
	{"id":"t-bbb222","repo":"wstein/workharbor","issue":"#8","state":"awaiting_guidance","agent_id":"a1"},
	{"id":"t-ccc333","repo":"wstein/workharbor","issue":"#7","state":"completed","agent_id":"a1"}
]`

func withTasks(s *stub) {
	s.reply("GET /v1/tasks", 200, ok(taskList))
}

func TestLsPrintsATableOfDataOnStdoutOnly(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	code, out, errOut := s.runCLI("", "ls")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"ID", "STATE", "t-aaa111", "awaiting_guidance", "wstein/workharbor"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	reqs := s.requests("GET /v1/tasks")
	if len(reqs) != 1 || reqs[0].query != "active=true" || reqs[0].header.Get("Authorization") != "Bearer "+tok {
		t.Errorf("request = %+v", reqs)
	}
	if code, _, _ := s.runCLI("", "ls", "--all"); code != 0 || s.requests("GET /v1/tasks")[1].query != "" {
		t.Errorf("--all must not filter: %+v", s.requests("GET /v1/tasks"))
	}
}

func TestJSONPrintsTheEnvelopeUnchanged(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/tasks", 200, ok(taskList))
	code, out, errOut := s.runCLI("", "ls", "--json")
	if code != 0 || errOut != "" || out != ok(taskList) {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	var env struct {
		SchemaVersion int  `json:"schema_version"`
		OK            bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || env.SchemaVersion != 1 || !env.OK {
		t.Errorf("not an envelope: %v", err)
	}
}

func TestRunSendsTheRequestWithAnIdempotencyKey(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/tasks", 201, ok(`{"task_id":"t-new","run_id":"r-new","held":false}`))
	code, out, errOut := s.runCLI("", "run", "https://github.com/wstein/workharbor/issues/7", "--agent", "docs-ws/docs", "--prompt", "be brief")
	if code != 0 || errOut != "" || out != "t-new\tr-new\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	reqs := s.requests("POST /v1/tasks")
	var body map[string]string
	_ = json.Unmarshal([]byte(reqs[0].body), &body)
	if body["issue_url"] != "https://github.com/wstein/workharbor/issues/7" || body["agent"] != "docs-ws/docs" || body["prompt"] != "be brief" || reqs[0].header.Get("Idempotency-Key") == "" {
		t.Errorf("request %+v", reqs[0])
	}
	// A retry with the same key is for the caller to choose.
	s.runCLI("", "run", "u", "--agent", "a/b", "--idempotency-key", "fixed-1")
	if got := s.requests("POST /v1/tasks")[1].header.Get("Idempotency-Key"); got != "fixed-1" {
		t.Errorf("key = %q", got)
	}
}

func TestUsageErrorsAreExitCode2AndSendNothing(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	for name, args := range map[string][]string{
		"run without an agent":  {"run", "https://github.com/a/b/issues/1"},
		"run without a url":     {"run", "--agent", "a/b"},
		"unknown command":       {"frobnicate"},
		"unknown flag":          {"ls", "--nope"},
		"too many arguments":    {"cancel", "a", "b"},
		"say without a message": {"say", "t-aaa"},
		"approve without arg":   {"approve"},
		"answer with one arg":   {"answer", "d1"},
		"root with an argument": {"stray"},
	} {
		code, out, errOut := s.runCLI("", args...)
		if code != exitcode.Usage || out != "" || !strings.HasPrefix(errOut, "whr: ") || strings.Count(errOut, "\n") != 1 {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", name, code, out, errOut)
		}
	}
	if len(s.requests("POST /v1/tasks")) != 0 {
		t.Error("a usage error sent a request")
	}
}

func TestSayReadsItsMessageFromAnArgumentAFileOrStandardInput(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	s.reply("POST /v1/tasks/t-aaa111/say", 200, ok(`{"delivery":"resumed_turn"}`))
	file := filepath.Join(t.TempDir(), "g.md")
	if err := os.WriteFile(file, []byte("from a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"say", "t-aaa", "from an argument"}},
		{"from std\n", []string{"say", "t-aaa", "-"}},
	} {
		code, out, errOut := s.runCLI(tc.stdin, tc.args...)
		if code != 0 || out != "resumed_turn\n" || errOut != "" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", tc.args, code, out, errOut)
		}
	}
	if code, _, _ := s.runCLI("", "say", "t-aaa", "-f", file); code != 0 {
		t.Errorf("-f: exit %d", code)
	}
	var got []string
	for _, r := range s.requests("POST /v1/tasks/t-aaa111/say") {
		var b map[string]string
		_ = json.Unmarshal([]byte(r.body), &b)
		got = append(got, b["message"])
		if r.header.Get("Idempotency-Key") == "" {
			t.Error("say sent no idempotency key")
		}
	}
	if strings.Join(got, "|") != "from an argument|from std\n|from a file\n" {
		t.Errorf("messages = %q", got)
	}
	if code, _, _ := s.runCLI("", "say", "t-aaa", "x", "-f", file); code != exitcode.Usage {
		t.Errorf("an argument and -f: exit %d, want usage", code)
	}
}

func TestTaskReferences(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	s.reply("POST /v1/tasks/t-aaa111/cancel", 200, ok(`{}`))
	s.reply("POST /v1/tasks/t-ccc333/cancel", 200, ok(`{}`))
	for ref, want := range map[string]string{
		"t-aaa111": "t-aaa111", "t-aaa": "t-aaa111", "wstein/workharbor#7": "t-aaa111", "#7": "t-aaa111", "t-ccc": "t-ccc333",
	} {
		code, out, errOut := s.runCLI("", "cancel", ref)
		if code != 0 || out != want+"\n" {
			t.Errorf("%q: exit %d, stdout %q, stderr %q, want %s", ref, code, out, errOut, want)
		}
	}
	if code, _, errOut := s.runCLI("", "cancel", "t-"); code != exitcode.Usage || !strings.Contains(errOut, "several") {
		t.Errorf("an ambiguous prefix: %d %q", code, errOut)
	}
	if code, _, errOut := s.runCLI("", "cancel", "nope"); code != exitcode.NotFound || !strings.Contains(errOut, "no task") {
		t.Errorf("an unknown ref: %d %q", code, errOut)
	}
}

func TestInboxStripsControlCharactersFromUntrustedText(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/inbox", 200, ok(`[{"id":"d1","task_id":"t1","kind":"question","subject":"clear \u001b[2J screen\nnext line","options":["a","b"]}]`))
	code, out, _ := s.runCLI("", "inbox")
	if code != 0 || strings.ContainsRune(out, 0x1b) || strings.Count(out, "\n") != 2 { // header and one row
		t.Errorf("exit %d, stdout %q", code, out)
	}
	if !strings.Contains(out, "clear ?[2J screen?next line") {
		t.Errorf("the subject was not sanitised: %q", out)
	}
}

func TestApproveNamesTheCommitForAReview(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/inbox", 200, ok(`[
		{"id":"d-review","task_id":"t1","kind":"review","subject":"Ready to push?","sha":"abcdef1234567890","options":["allow","deny"]},
		{"id":"d-tool","task_id":"t1","kind":"approval","subject":"Bash","options":["allow","deny"]},
		{"id":"d-q","task_id":"t1","kind":"question","subject":"Which?","options":["a","b"]}]`))
	for _, id := range []string{"d-review", "d-tool", "d-q"} {
		s.reply("POST /v1/decisions/"+id+"/answer", 200, ok(`{}`))
	}
	code, _, errOut := s.runCLI("", "approve", "d-review")
	if code != exitcode.Usage || !strings.Contains(errOut, "--sha abcdef123456") {
		t.Errorf("a review without --sha: exit %d, stderr %q", code, errOut)
	}
	if len(s.requests("POST /v1/decisions/d-review/answer")) != 0 {
		t.Error("an unconfirmed review was answered")
	}
	if code, out, _ := s.runCLI("", "approve", "d-review", "--sha", "abcdef1234567890"); code != 0 || out != "d-review\n" {
		t.Errorf("approve with --sha: %d %q", code, out)
	}
	var b map[string]string
	_ = json.Unmarshal([]byte(s.requests("POST /v1/decisions/d-review/answer")[0].body), &b)
	if b["option"] != "allow" || b["sha"] != "abcdef1234567890" {
		t.Errorf("body = %v", b)
	}
	if code, _, _ := s.runCLI("", "approve", "d-tool"); code != 0 {
		t.Errorf("a tool approval needs no sha: %d", code)
	}
	if code, _, _ := s.runCLI("", "reject", "d-tool", "--reason", "no network"); code != 0 {
		t.Errorf("reject: %d", code)
	}
	_ = json.Unmarshal([]byte(s.requests("POST /v1/decisions/d-tool/answer")[1].body), &b)
	if b["option"] != "deny" || b["reason"] != "no network" {
		t.Errorf("reject body = %v", b)
	}
	s.reply("POST /v1/decisions/d-q/answer", 200, ok(`{"new_run_id":"r2"}`))
	if code, out, _ := s.runCLI("", "answer", "d-q", "a"); code != 0 || out != "d-q\tr2\n" {
		t.Errorf("answer = %d %q", code, out)
	}
}

func TestErrorsKeepTheServersExitCodeAndPrintOneLineOnStderr(t *testing.T) {
	for name, tc := range map[string]struct {
		status, exit int
		code         string
	}{
		"not found": {404, exitcode.NotFound, "not_found"},
		"conflict":  {409, exitcode.Conflict, "conflict"},
		"auth":      {401, exitcode.Auth, "unauthorized"},
		"usage":     {400, exitcode.Usage, "usage"},
		"timeout":   {504, exitcode.Timeout, "timeout"},
		"internal":  {500, exitcode.Error, "error"},
	} {
		s := newStub(t)
		s.reply("GET /v1/tasks", tc.status, fail(tc.code, tc.exit, "it\nwent \x1b[31mwrong"))
		code, out, errOut := s.runCLI("", "ls")
		if code != tc.exit || out != "" || strings.Count(errOut, "\n") != 1 || strings.ContainsRune(errOut, 0x1b) || !strings.Contains(errOut, "it went ?[31mwrong") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", name, code, out, errOut)
		}
		if strings.Contains(errOut, tok) {
			t.Errorf("%s: the token is in the error", name)
		}
	}
}

func TestAnUnreachableServerIsAnErrorWithAHint(t *testing.T) {
	var out, errOut bytes.Buffer
	env := Env{
		Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" },
		NewClient: func(string) (*Client, error) { return NewClientFor("http://127.0.0.1:1", tok), nil },
	}
	code := Execute(context.Background(), env, []string{"ls"})
	if code != exitcode.Error || out.Len() != 0 || !strings.Contains(errOut.String(), "whr serve") || strings.Contains(errOut.String(), "127.0.0.1:1/") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

func TestEventsAndJSONOutputAreInert(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	// Raw DEL, C1 CSI and a bidi override in event data and kind, as an agent could write them.
	body := `{"seq":1,"task_id":"t-aaa111","kind":"x\u001b[2J","tier":"audit","at":"2026-10-01T09:00:00Z","data":{"m":"a` + "\u009b31m\u202eb\u007f" + `"}}`
	s.h["GET /v1/tasks/t-aaa111/log"] = func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.URL.Query().Get("since") != "0" {
			_, _ = io.WriteString(w, ok("[]"))
			return
		}
		_, _ = io.WriteString(w, ok("["+body+"]"))
	}
	bad := "\x1b\x7f\u009b\u202e"
	code, out, errOut := s.runCLI("", "logs", "t-aaa")
	if code != 0 || strings.ContainsAny(out, bad) || !strings.Contains(out, "a?31m?b?") {
		t.Errorf("human events are not inert: %d %q %q", code, out, errOut)
	}
	_, out, _ = s.runCLI("", "logs", "t-aaa", "--json")
	var ev map[string]any
	if strings.ContainsAny(out, bad) || json.Unmarshal([]byte(out), &ev) != nil || ev["kind"] != "x\x1b[2J" {
		t.Errorf("JSON events are not inert or not valid: %q", out)
	}
	s.reply("GET /v1/inbox", 200, ok(`[{"id":"d1","task_id":"t1","kind":"question","subject":"s`+"\u009b\u202e"+`\u001b]0;x\u0007","options":[]}]`))
	_, out, _ = s.runCLI("", "inbox", "--json")
	if strings.ContainsAny(out, bad) || !json.Valid([]byte(out)) {
		t.Errorf("--json envelope is not inert: %q", out)
	}
}

func TestLogsDumpsPagesAndFollowsWithReconnect(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	page := func(from, n int) string {
		var evs []string
		for i := 0; i < n; i++ {
			evs = append(evs, fmt.Sprintf(`{"seq":%d,"task_id":"t-aaa111","kind":"run.state","tier":"audit","at":"2026-10-01T09:00:00Z","data":{"n":%d}}`, from+i, from+i))
		}
		return ok("[" + strings.Join(evs, ",") + "]")
	}
	s.h["GET /v1/tasks/t-aaa111/log"] = func(w http.ResponseWriter, r *http.Request, _ string) {
		since := r.URL.Query().Get("since")
		switch since {
		case "0":
			_, _ = io.WriteString(w, page(1, 500))
		default:
			_, _ = io.WriteString(w, page(501, 3))
		}
	}
	code, out, errOut := s.runCLI("", "logs", "t-aaa")
	if code != 0 || errOut != "" || strings.Count(out, "\n") != 503 || !strings.HasPrefix(out, "1\t2026-10-01T09:00:00Z\trun.state\t{\"n\":1}\n") {
		t.Fatalf("exit %d, %d lines, stderr %q", code, strings.Count(out, "\n"), errOut)
	}
	if code, out, _ := s.runCLI("", "logs", "t-aaa", "--json"); code != 0 || !strings.HasPrefix(out, `{"seq":1,`) {
		t.Errorf("--json lines: %d %.60q", code, out)
	}

	// Following: the first connection delivers two events and drops; the second
	// must start after the last one and deliver a third.
	var conns []string
	var mu sync.Mutex
	s.h["GET /v1/tasks/t-aaa111/events"] = func(w http.ResponseWriter, r *http.Request, _ string) {
		mu.Lock()
		conns = append(conns, r.Header.Get("Last-Event-ID"))
		n := len(conns)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		ev := func(seq int) {
			_, _ = fmt.Fprintf(w, "id: %d\nevent: run.state\ndata: {\"seq\":%d,\"kind\":\"run.state\",\"tier\":\"audit\",\"at\":\"2026-10-01T09:00:00Z\"}\n\n", seq, seq)
		}
		_, _ = io.WriteString(w, ": connected\n\n")
		if n == 1 {
			ev(10)
			ev(11)
			return // drop
		}
		ev(12)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	var buf, errBuf safeBuf
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		env := Env{Stdout: &buf, Stderr: &errBuf, Getenv: func(string) string { return "" }, NewClient: func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil }}
		done <- Execute(ctx, env, []string{"logs", "-f", "t-aaa", "--since", "9"})
	}()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(buf.String(), "\n") >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("an interrupted follow exits %d, want 0", code)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(conns) < 2 || conns[0] != "9" || conns[1] != "11" {
		t.Errorf("Last-Event-ID of the connections = %q, want 9 then 11", conns)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "10\t") || !strings.HasPrefix(lines[2], "12\t") {
		t.Errorf("events printed: %q", lines)
	}
	if !strings.Contains(errBuf.String(), "reconnecting from event 11") {
		t.Errorf("no reconnect notice on stderr: %q", errBuf.String())
	}
}

func TestShellCompletionOffersTaskDecisionAndAgentIDs(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	s.reply("GET /v1/inbox", 200, ok(`[{"id":"d-1","task_id":"t1","kind":"question","subject":"Which?","options":["alpha","beta"]}]`))
	s.reply("GET /v1/workspaces", 200, ok(`[{"id":"w1","name":"docs-ws","repo":"a/b","integration":"main","agents":[{"id":"a1","role":"docs","branch":"agent/docs"}]}]`))
	for args, want := range map[string]string{
		"say ":         "t-aaa111",
		"cancel ":      "t-bbb222",
		"logs ":        "t-ccc333",
		"approve ":     "d-1",
		"answer d-1 ":  "alpha",
		"run --agent ": "docs-ws/docs",
	} {
		_, out, _ := s.runCLI("", append([]string{"__complete"}, append(strings.Fields(args), "")...)...)
		if !strings.Contains(out, want) {
			t.Errorf("completion for %q = %q, want it to offer %s", args, out, want)
		}
	}
}

// serveOnSocket serves the stub's handler on a unix socket in a short private
// directory, as `whr serve` serves the API, and returns the directory (the
// configuration's state_dir).
func serveOnSocket(t *testing.T, s *stub) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "whr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", filepath.Join(dir, "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(s.ts.Config.Handler)
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return dir
}

func TestTheConfigurationFileGivesTheSocketAndTheToken(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	state := serveOnSocket(t, s)
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "api.token")
	if err := os.WriteFile(tokenFile, []byte(tok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(fmt.Sprintf(`{"listen":"127.0.0.1:8787","state_dir":%q,"api_token_file":%q,"roots":{"x":1}}`, state, tokenFile)), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut, Getenv: func(k string) string {
		if k == "WHR_CONFIG" {
			return cfg
		}
		return ""
	}}
	if code := Execute(context.Background(), env, []string{"ls"}); code != 0 || !strings.Contains(out.String(), "t-aaa111") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	if got := s.requests("GET /v1/tasks")[0].header.Get("Authorization"); got != "Bearer "+tok {
		t.Errorf("the token from the file was not sent: %q", got)
	}
	// A token file other people can read is refused.
	if err := os.Chmod(tokenFile, 0o644); err != nil { //nolint:gosec // a deliberately loose file
		t.Fatal(err)
	}
	if code := Execute(context.Background(), env, []string{"ls"}); code == 0 || strings.Contains(errOut.String(), tok) {
		t.Errorf("a loose token file: exit %d, stderr %q", code, errOut.String())
	}
	// --config wins over the environment.
	if code := Execute(context.Background(), env, []string{"--config", filepath.Join(dir, "missing.json"), "ls"}); code == 0 {
		t.Error("--config was ignored")
	}
}

// The client sends its token only to the API's unix socket (D29, §7.5): whatever
// the configuration's listen address is, nothing is ever sent to it, so the token
// never crosses a network, not even loopback.
func TestTheClientSendsTheTokenOnlyToTheSocket(t *testing.T) {
	var hits int
	web := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	t.Cleanup(web.Close)
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "api.token")
	if err := os.WriteFile(tokenFile, []byte(tok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty, err := os.MkdirTemp("", "whr") // no socket in it
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(empty) })
	cfg := filepath.Join(dir, "c.json")
	raw, _ := json.Marshal(map[string]string{"listen": strings.TrimPrefix(web.URL, "http://"), "state_dir": empty, "api_token_file": tokenFile})
	if err := os.WriteFile(cfg, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Do(context.Background(), "GET", "/v1/tasks", nil, "")
	var ce connError
	if !errors.As(err, &ce) || strings.Contains(err.Error(), tok) {
		t.Fatalf("a missing socket must be a connection error that hides the token: %v", err)
	}
	if hits != 0 {
		t.Errorf("the listen address got %d request(s): the token went over TCP", hits)
	}
}

// An issue by an untrusted author starts nothing: the task is held, the data is
// printed, and the exit code says a human has to answer.
func TestRunOnAnUntrustedIssuePrintsTheHoldAndExitsSix(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/tasks", 202, ok(`{"task_id":"t-held","held":true,"decision_id":"d-hold"}`))
	code, out, errOut := s.runCLI("", "run", "https://github.com/a/b/issues/8", "--agent", "ws/docs")
	if code != exitcode.NeedsHuman || out != "t-held\t\theld\td-hold\n" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if !strings.Contains(errOut, "whr answer d-hold start") || strings.Count(errOut, "\n") != 1 {
		t.Errorf("stderr %q", errOut)
	}
	if code, out, _ := s.runCLI("", "run", "https://github.com/a/b/issues/8", "--agent", "ws/docs", "--json"); code != exitcode.NeedsHuman || !strings.Contains(out, `"held":true`) {
		t.Errorf("--json: exit %d, stdout %q", code, out)
	}
}

func TestDoctorSaysNotVerifiedAndFailsOnlyOnFail(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	// no configuration file at the default path: the config check fails
	code, out, _ := s.runCLI("", "doctor", "--skip", "runtime")
	if code != exitcode.Error || !strings.Contains(out, "fail\tconfig\t") || !strings.Contains(out, "skipped\truntime\t") || !strings.Contains(out, "not_verified\tegress\t") {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if strings.Contains(out, "ok\tegress") {
		t.Error("egress reported ok")
	}
	code, out, _ = s.runCLI("", "doctor", "--json", "--skip", "runtime")
	var v struct {
		OK     bool
		Checks []struct{ Check, Status string }
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || code != exitcode.Error || v.OK || len(v.Checks) == 0 {
		t.Fatalf("exit %d, %v, %q", code, err, out)
	}
	if code, _, _ := s.runCLI("", "doctor", "--skip", "nope"); code != exitcode.Usage {
		t.Errorf("unknown check: exit %d, want usage", code)
	}
}

func TestKillAllAsksFirstAndSaysWhatItDid(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/kill-all", 200, ok(`{"cancelled":["t-aaa111","t-bbb222"],"tokens_revoked":2,"problems":[]}`))
	// Anything but the word stops before the request.
	for _, in := range []string{"", "yes\n", "kill\n"} {
		if code, out, errOut := s.runCLI(in, "kill-all"); code != exitcode.Usage || out != "" || !strings.Contains(errOut, "not confirmed") {
			t.Errorf("input %q: exit %d, stdout %q, stderr %q", in, code, out, errOut)
		}
	}
	if len(s.requests("POST /v1/kill-all")) != 0 {
		t.Fatal("a request went out without a confirmation")
	}
	code, out, errOut := s.runCLI("kill-all\n", "kill-all")
	if code != 0 || out != "t-aaa111\nt-bbb222\n" || !strings.Contains(errOut, "cancelled 2 task(s), revoked 2 forge token(s)") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if reqs := s.requests("POST /v1/kill-all"); len(reqs) != 1 || !strings.Contains(reqs[0].body, `"confirm":true`) || reqs[0].header.Get("Idempotency-Key") == "" {
		t.Errorf("requests = %+v", reqs)
	}
	if code, _, _ := s.runCLI("", "kill-all", "--yes"); code != 0 {
		t.Errorf("--yes: exit %d", code)
	}
}

func TestKillAllSaysWhatFailedAndExitsNonZero(t *testing.T) {
	s := newStub(t)
	s.reply("POST /v1/kill-all", 200, ok(`{"cancelled":["t-aaa111"],"tokens_revoked":0,"problems":["revoke the token for a/b: 500"]}`))
	code, out, errOut := s.runCLI("", "kill-all", "--yes")
	if code != exitcode.Error || out != "t-aaa111\n" || !strings.Contains(errOut, "problem: revoke the token for a/b: 500") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

const usageData = `{"group":"task","rows":[
{"key":"t-aaa111","auth":"subscription","turns":3,"tokens":{"input":18,"output":194,"cache_read":43502,"cache_write":233},"turns_without_tokens":0,"reported_micro_usd":5804,"turns_without_cost":0,"notional":true},
{"key":"t-aaa111","auth":"api-key","turns":2,"tokens":{"input":0,"output":0,"cache_read":0,"cache_write":0},"turns_without_tokens":2,"reported_micro_usd":0,"turns_without_cost":2,"notional":false}],
"windows":[{"account":"claude","name":"five_hour","utilization":0.42,"resets_at":"2026-10-01T15:00:00Z","at":"2026-10-01T12:00:00Z"}],
"balance":{"agent":"claude","remaining_micro_usd":12340000,"at":"2026-10-01T12:00:00Z"}}`

func TestUsagePrintsRowsAndLeadsWithTheWindowOnASubscription(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	s.reply("GET /v1/usage", 200, ok(usageData))
	code, out, errOut := s.runCLI("", "usage")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if i, j := strings.Index(out, "five_hour"), strings.Index(out, "t-aaa111"); i < 0 || j < 0 || i > j {
		t.Errorf("a subscription leads with its window:\n%s", out)
	}
	for _, want := range []string{"42%", "$0.0058 reported, notional", "not reported", "2 turn(s) without tokens", "43.8k", "balance: $12.3400 left"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	// --json is the envelope as the API sent it.
	code, out, _ = s.runCLI("", "usage", "--json")
	if code != 0 || !strings.Contains(out, `"schema_version":1`) || !strings.Contains(out, `"remaining_micro_usd":12340000`) {
		t.Errorf("--json: exit %d, %q", code, out)
	}
}

func TestUsageSendsTheFiltersAndRefusesBadOnes(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	s.reply("GET /v1/usage", 200, ok(`{"group":"day","rows":[],"windows":[]}`))
	if code, _, errOut := s.runCLI("", "usage", "--task", "t-aaa", "--repo", "wstein/workharbor", "--since", "2026-10-01", "--until", "2026-10-02T00:00:00Z", "--by", "day"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	reqs := s.requests("GET /v1/usage")
	if len(reqs) != 1 {
		t.Fatalf("requests = %+v", reqs)
	}
	q, _ := url.ParseQuery(reqs[0].query)
	if q.Get("task") != "t-aaa111" || q.Get("repo") != "wstein/workharbor" || q.Get("since") != "2026-10-01T00:00:00Z" || q.Get("until") != "2026-10-02T00:00:00Z" || q.Get("by") != "day" {
		t.Errorf("query = %v", q)
	}
	// "7d" and "36h" are how long ago: a time in the past, in RFC 3339.
	if code, _, _ := s.runCLI("", "usage", "--since", "7d"); code != 0 {
		t.Fatal("--since 7d")
	}
	reqs = s.requests("GET /v1/usage")
	since, err := time.Parse(time.RFC3339, func() string { q, _ := url.ParseQuery(reqs[len(reqs)-1].query); return q.Get("since") }())
	if err != nil || time.Since(since) < 6*24*time.Hour || time.Since(since) > 8*24*time.Hour {
		t.Errorf("--since 7d = %v, %v", since, err)
	}
	for _, args := range [][]string{{"usage", "--since", "yesterday"}, {"usage", "--by", "week"}, {"usage", "--task", "nope"}, {"usage", "extra"}} {
		if code, _, _ := s.runCLI("", args...); code == 0 {
			t.Errorf("%v was accepted", args)
		}
	}
}

func TestParseWhenDollarsAndCounts(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"2026-10-01": "2026-10-01T00:00:00Z", "2026-10-01T05:06:07+02:00": "2026-10-01T03:06:07Z", "7d": "2026-10-01T12:00:00Z", "36h": "2026-10-07T00:00:00Z", "0d": "2026-10-08T12:00:00Z",
	} {
		got, err := parseWhen(in, now)
		if err != nil || got.Format(time.RFC3339) != want {
			t.Errorf("parseWhen(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "yesterday", "-1d", "1x", "2026-13-01"} {
		if _, err := parseWhen(bad, now); err == nil {
			t.Errorf("parseWhen(%q) was accepted", bad)
		}
	}
	for micro, want := range map[int64]string{0: "$0.0000", 5804: "$0.0058", 999_999: "$1.0000", 12_000_000: "$12.0000"} {
		if got := dollars(micro); got != want {
			t.Errorf("dollars(%d) = %s", micro, got)
		}
	}
	for n, want := range map[int64]string{999: "999", 1500: "1.5k", 2_500_000: "2.5M"} {
		if got := count(n); got != want {
			t.Errorf("count(%d) = %s", n, got)
		}
	}
}

func TestUsageByAgentAndModelShowsCacheAndKeepsEstimatesApart(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/usage", 200, ok(`{"group":"agent","rows":[
{"key":"docs/review","auth":"api-key","turns":2,"tokens":{"input":100,"output":50,"cache_read":300,"cache_write":0},"turns_without_tokens":0,"reported_micro_usd":5000,"estimated_micro_usd":700,"turns_without_cost":0,"notional":false,"cache_share":0.75},
{"key":"docs/review","auth":"subscription","turns":1,"tokens":{"input":0,"output":0,"cache_read":0,"cache_write":0},"turns_without_tokens":1,"reported_micro_usd":0,"estimated_micro_usd":0,"turns_without_cost":1,"notional":true}],"windows":[]}`))
	for _, by := range []string{"agent", "model", "day"} {
		if code, _, errOut := s.runCLI("", "usage", "--by", by); code != 0 {
			t.Errorf("--by %s: exit %d, %s", by, code, errOut)
		}
	}
	_, out, _ := s.runCLI("", "usage", "--by", "agent")
	for _, want := range []string{"AGENT", "CACHE", "75%", "$0.0050 reported, $0.0007 estimated", "API-equivalent, not billed"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	var sent []string
	for _, r := range s.requests("GET /v1/usage") {
		sent = append(sent, r.query)
	}
	if !slices.Contains(sent, "by=agent") || !slices.Contains(sent, "by=model") || !slices.Contains(sent, "by=day") {
		t.Errorf("queries sent = %v", sent)
	}
}

func TestShowPrintsTheReviewCardAndTheUsageLine(t *testing.T) {
	s := newStub(t)
	withTasks(s)
	body := `{"id":"t-aaa111","repo":"wstein/workharbor","issue":"#7","state":"awaiting_review","runs":[],` +
		`"open_decisions":[{"id":"d-1","task_id":"t-aaa111","kind":"review","subject":"Review abc123"}],` +
		`"candidate":{"branch":"agent/docs","sha":"abc123","ci":"passed","pushed":false,"files":3,"added":40,"removed":7},` +
		`"usage_line":"usage: 3 turns"}`
	s.reply("GET /v1/tasks/t-aaa111", 200, ok(body))
	code, out, errOut := s.runCLI("", "show", "t-aaa111")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"3 file(s), +40 -7", "CI:       passed (for abc123)", "Usage:    usage: 3 turns", "Tests:    not available", "d-1  review  Review abc123"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = s.runCLI("", "--json", "show", "t-aaa111")
	if code != 0 || !strings.Contains(out, `"usage_line":"usage: 3 turns"`) {
		t.Errorf("--json must print the envelope unchanged: %d %s", code, out)
	}
}
