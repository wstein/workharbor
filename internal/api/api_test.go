package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const token = "s3cret-token-value"

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// fake is a Backend whose answers a test sets.
type fake struct {
	mu       sync.Mutex
	runs     int
	since    []int64
	logCalls [][2]int64
	events   chan domain.Event

	onRun    func(service.RunRequest) (domain.ID, domain.ID, error)
	onShow   func(domain.ID) (service.TaskView, error)
	onSay    func(domain.ID, string) (agent.Delivery, error)
	onCancel func(domain.ID) error
	onAnswer func(domain.ID, domain.Response) (domain.ID, error)
}

func (f *fake) List(_ context.Context, active bool) ([]store.TaskSummary, error) {
	list := []store.TaskSummary{
		{ID: "t2", Repo: "wstein/workharbor", Issue: "#2", State: domain.TaskRunning, AgentID: "a1", CreatedAt: t0.Add(time.Minute)},
		{ID: "t1", Repo: "wstein/workharbor", Issue: "#1", State: domain.TaskCompleted, AgentID: "a1", CreatedAt: t0},
	}
	if active {
		return list[:1], nil
	}
	return list, nil
}

func (f *fake) Show(_ context.Context, id domain.ID) (service.TaskView, error) {
	if f.onShow != nil {
		return f.onShow(id)
	}
	if id != "t2" {
		return service.TaskView{}, &domain.NotFoundError{Kind: "task", ID: string(id)}
	}
	return service.TaskView{
		Task:      domain.Task{ID: "t2", Repo: "wstein/workharbor", Issue: "#2", State: domain.TaskAwaitingGuidance, AgentID: "a1", CreatedAt: t0},
		Runs:      []domain.Run{{ID: "r1", AgentID: "a1", EnvID: "e1", State: domain.RunRunning, SessionID: "sess-1"}},
		Open:      []domain.Decision{{ID: "d1", TaskID: "t2", RunID: "r1", Kind: domain.DecisionQuestion, Blocking: true, Subject: "Which file?", Input: "a.go\nb.go", Options: []string{"a", "b"}, Status: domain.DecisionOpen}},
		Candidate: &domain.ReviewCandidate{Branch: "agent/docs", SHA: "abc123", CI: domain.CIPending},
	}, nil
}

func (f *fake) Run(_ context.Context, req service.RunRequest) (domain.ID, domain.ID, error) {
	f.mu.Lock()
	f.runs++
	f.mu.Unlock()
	if f.onRun != nil {
		return f.onRun(req)
	}
	return "t3", "r3", nil
}

func (f *fake) Say(_ context.Context, id domain.ID, m string) (agent.Delivery, error) {
	if f.onSay != nil {
		return f.onSay(id, m)
	}
	return agent.DeliveryNextTurn, nil
}

func (f *fake) Cancel(_ context.Context, id domain.ID) error {
	if f.onCancel != nil {
		return f.onCancel(id)
	}
	return nil
}

func (f *fake) Answer(_ context.Context, id domain.ID, r domain.Response) (domain.ID, error) {
	if f.onAnswer != nil {
		return f.onAnswer(id, r)
	}
	return "", nil
}

func (f *fake) Inbox(context.Context) ([]domain.Decision, error) {
	return []domain.Decision{{ID: "d1", TaskID: "t2", Kind: domain.DecisionReview, Blocking: true, Subject: "Ready to push? 2 commits", SHA: "abc123", Options: []string{"allow", "deny"}, Status: domain.DecisionOpen}}, nil
}

func (f *fake) WorkspaceList(context.Context) ([]service.WorkspaceView, error) {
	return []service.WorkspaceView{{
		Workspace: domain.Workspace{ID: "w1", Name: "docs-ws", Repo: "wstein/workharbor", Integration: "main"},
		Agents:    []domain.Agent{{ID: "a1", Role: "docs", Branch: "agent/docs"}},
	}}, nil
}

func (f *fake) Log(_ context.Context, id domain.ID, since int64, limit int) ([]domain.Event, error) {
	if id != "t2" {
		return nil, &domain.NotFoundError{Kind: "task", ID: string(id)}
	}
	f.mu.Lock()
	f.logCalls = append(f.logCalls, [2]int64{since, int64(limit)})
	f.mu.Unlock()
	return []domain.Event{
		{Seq: since + 1, TaskID: "t2", Kind: domain.EventRunStarted, Tier: domain.TierAudit, At: t0, Payload: []byte(`{"run_id":"r1"}`)},
		{Seq: since + 2, TaskID: "t2", Kind: domain.EventTranscript, Tier: domain.TierTranscript, At: t0, Payload: []byte(`{"kind":"message","text":"hello"}`)},
	}, nil
}

func (f *fake) Subscribe(_ context.Context, _ domain.ID, since int64) (<-chan domain.Event, error) {
	f.mu.Lock()
	f.since = append(f.since, since)
	f.mu.Unlock()
	if f.events == nil {
		f.events = make(chan domain.Event, 16)
	}
	return f.events, nil
}

type rig struct {
	t    *testing.T
	be   *fake
	srv  *Server
	ts   *httptest.Server
	st   *store.Store
	errs []error
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &rig{t: t, be: &fake{}, st: st}
	r.srv, err = New(r.be, Options{Token: []byte(token), Store: st, Heartbeat: 50 * time.Millisecond, Now: func() time.Time { return t0 }, OnError: func(e error) { r.errs = append(r.errs, e) }})
	if err != nil {
		t.Fatal(err)
	}
	r.ts = httptest.NewServer(r.srv.Handler())
	t.Cleanup(r.ts.Close)
	return r
}

// do sends a request with the token unless the header map says otherwise.
func (r *rig) do(method, path, body string, hdr ...string) (int, http.Header, string) {
	r.t.Helper()
	req, err := http.NewRequest(method, r.ts.URL+path, strings.NewReader(body)) //nolint:noctx // a test
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			req.Header.Del(hdr[i])
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

// golden compares a response with testdata/golden/<name>.json (the status line
// and the body), and rewrites it with -update.
func golden(t *testing.T, name string, status int, body string) {
	t.Helper()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(body), "", "  "); err != nil {
		t.Fatalf("%s: the body is not JSON: %v\n%s", name, err, body)
	}
	got := "HTTP " + http.StatusText(status) + " (" + itoa(status) + ")\n" + pretty.String() + "\n"
	path := filepath.Join("testdata", "golden", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // a fixture in the repository
	if err != nil {
		t.Fatalf("%s: %v (run with -update to create it)", name, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden file:\n--- want\n%s--- got\n%s", name, want, got)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// ---- the contract ----

func TestEveryRouteIsInTheDocumentAndTheOtherWayRound(t *testing.T) {
	var doc struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPI(), &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Errorf("openapi = %q, want 3.1.x", doc.OpenAPI)
	}
	inDoc := map[string]bool{}
	ids := map[string]string{}
	for path, methods := range doc.Paths {
		for m, op := range methods {
			key := strings.ToUpper(m) + " " + path
			inDoc[key] = true
			if op.OperationID == "" {
				t.Errorf("%s has no operationId", key)
			}
			if prev, dup := ids[op.OperationID]; dup {
				t.Errorf("operationId %q is used by %s and %s", op.OperationID, prev, key)
			}
			ids[op.OperationID] = key
		}
	}
	inCode := map[string]bool{}
	for _, r := range Routes() {
		inCode[r] = true
	}
	for r := range inDoc {
		if !inCode[r] {
			t.Errorf("%s is in the document and has no handler", r)
		}
	}
	for r := range inCode {
		if !inDoc[r] {
			t.Errorf("%s has a handler and is not in the document", r)
		}
	}
}

func TestTheContractTestNoticesDrift(t *testing.T) {
	// A copy of the table with one route missing must be reported: the check
	// above is only worth something if it can fail.
	saved := routes
	defer func() { routes = saved }()
	routes = routes[1:]
	inCode := map[string]bool{}
	for _, r := range Routes() {
		inCode[r] = true
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	_ = json.Unmarshal(OpenAPI(), &doc)
	missing := 0
	for path, ms := range doc.Paths {
		for m := range ms {
			if !inCode[strings.ToUpper(m)+" "+path] {
				missing++
			}
		}
	}
	if missing != 1 {
		t.Errorf("%d routes missing from the code, want 1", missing)
	}
}

func TestTheOpenAPIDocumentIsServed(t *testing.T) {
	r := newRig(t)
	status, hdr, body := r.do("GET", "/v1/openapi.json", "")
	if status != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") || !json.Valid([]byte(body)) || !strings.Contains(body, `"3.1.0"`) {
		t.Errorf("status %d, type %q", status, hdr.Get("Content-Type"))
	}
}

// ---- the token ----

func TestEveryRouteNeedsTheToken(t *testing.T) {
	r := newRig(t)
	var paths []string
	for _, rt := range Routes() {
		m, p, _ := strings.Cut(rt, " ")
		paths = append(paths, m+" "+strings.NewReplacer("{task}", "t2", "{decision}", "d1").Replace(p))
	}
	paths = append(paths, "GET /nothing/here", "DELETE /v1/tasks")
	for _, p := range paths {
		m, path, _ := strings.Cut(p, " ")
		for name, hdr := range map[string][]string{
			"no token":     {"Authorization", ""},
			"wrong token":  {"Authorization", "Bearer " + token + "x"},
			"short token":  {"Authorization", "Bearer s3"},
			"wrong scheme": {"Authorization", "Basic " + token},
			"bare token":   {"Authorization", token},
			"empty bearer": {"Authorization", "Bearer "},
		} {
			status, _, body := r.do(m, path, "{}", hdr...)
			if status != http.StatusUnauthorized || !strings.Contains(body, `"exit_code":4`) {
				t.Errorf("%s with %s = %d %s", p, name, status, body)
			}
			if strings.Contains(body, token) {
				t.Errorf("%s with %s echoed the token", p, name)
			}
		}
	}
	golden(t, "unauthorized", 401, func() string { _, _, b := r.do("GET", "/v1/tasks", "", "Authorization", ""); return b }())
}

func TestANewServerNeedsATokenAndAStore(t *testing.T) {
	if _, err := New(&fake{}, Options{Store: &store.Store{}}); err == nil {
		t.Error("an empty token was accepted")
	}
	if _, err := New(&fake{}, Options{Token: []byte("x")}); err == nil {
		t.Error("no store was accepted")
	}
}

func TestTokenFromConfig(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "api.token")
	if err := os.WriteFile(good, []byte("abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := TokenFromConfig(&config.Config{APITokenFile: good})
	if err != nil || string(tok) != "abc123" {
		t.Errorf("token = %q, %v", tok, err)
	}
	loose := filepath.Join(dir, "loose.token")
	if err := os.WriteFile(loose, []byte("abc"), 0o644); err != nil { //nolint:gosec // a deliberately loose file
		t.Fatal(err)
	}
	if _, err := TokenFromConfig(&config.Config{APITokenFile: loose}); err == nil {
		t.Error("a world-readable token file was accepted")
	}
	empty := filepath.Join(dir, "empty.token")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := TokenFromConfig(&config.Config{APITokenFile: empty}); err == nil {
		t.Error("an empty token was accepted")
	}
}

// ---- loopback ----

func TestListenRefusesEverythingButLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.5:8787", "localhost:0", "example.com:80", "[::]:0", "nonsense"} {
		if ln, err := Listen(addr); err == nil {
			_ = ln.Close()
			t.Errorf("Listen(%q) was accepted", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
		ln, err := Listen(addr)
		if err != nil {
			t.Logf("Listen(%q) = %v (no such address here?)", addr, err)
			continue
		}
		_ = ln.Close()
	}
}

// ---- the envelope ----

func TestTheEnvelopeAndItsExitCodes(t *testing.T) {
	r := newRig(t)
	for name, tc := range map[string]struct {
		method, path, body string
	}{
		"list":          {"GET", "/v1/tasks", ""},
		"list-active":   {"GET", "/v1/tasks?active=true", ""},
		"show":          {"GET", "/v1/tasks/t2", ""},
		"inbox":         {"GET", "/v1/inbox", ""},
		"workspaces":    {"GET", "/v1/workspaces", ""},
		"run":           {"POST", "/v1/tasks", `{"issue_url":"https://github.com/wstein/workharbor/issues/7","agent":"docs-ws/docs"}`},
		"say":           {"POST", "/v1/tasks/t2/say", `{"message":"use the helper"}`},
		"cancel":        {"POST", "/v1/tasks/t2/cancel", ``},
		"answer":        {"POST", "/v1/decisions/d1/answer", `{"option":"allow","sha":"abc123"}`},
		"not-found":     {"GET", "/v1/tasks/nope", ""},
		"unknown-route": {"GET", "/v1/nothing", ""},
		"wrong-method":  {"DELETE", "/v1/tasks", ""},
		"bad-body":      {"POST", "/v1/tasks/t2/say", `{"message":`},
		"unknown-field": {"POST", "/v1/tasks/t2/say", `{"message":"x","extra":1}`},
		"two-values":    {"POST", "/v1/tasks/t2/say", `{"message":"x"}{"message":"y"}`},
		"empty-message": {"POST", "/v1/tasks/t2/say", `{"message":"  "}`},
		"empty-option":  {"POST", "/v1/decisions/d1/answer", `{"option":""}`},
		"bad-since":     {"GET", "/v1/tasks/t2/events?since=abc", ""},
		"log":           {"GET", "/v1/tasks/t2/log?since=4&limit=10", ""},
		"log-not-found": {"GET", "/v1/tasks/nope/log", ""},
		"log-bad-limit": {"GET", "/v1/tasks/t2/log?limit=0", ""},
	} {
		status, _, body := r.do(tc.method, tc.path, tc.body)
		golden(t, name, status, body)
	}

	// The exit code maps to the status, and the message of an unexpected error
	// is generic.
	for code, want := range map[int]int{
		exitcode.Usage: 400, exitcode.NotFound: 404, exitcode.Auth: 401, exitcode.Conflict: 409,
		exitcode.NeedsHuman: 409, exitcode.Timeout: 504, exitcode.Error: 500, exitcode.TaskFailed: 500,
	} {
		if got := statusFor(code); got != want {
			t.Errorf("exit code %d -> %d, want %d", code, got, want)
		}
	}
	r.be.onCancel = func(domain.ID) error { return errors.New("sql: database is locked at /Users/h/secret/path") }
	status, _, body := r.do("POST", "/v1/tasks/t2/cancel", "")
	if status != 500 || strings.Contains(body, "secret") || strings.Contains(body, "locked") {
		t.Errorf("an internal error leaked: %d %s", status, body)
	}
	golden(t, "internal-error", status, body)
	if len(r.errs) != 1 {
		t.Errorf("OnError heard %d errors, want 1", len(r.errs))
	}
	r.be.onCancel = func(domain.ID) error { return domain.NewConflict("test", "task is cancelled already") }
	status, _, body = r.do("POST", "/v1/tasks/t2/cancel", "")
	golden(t, "conflict", status, body)
}

func TestARequestBodyIsBounded(t *testing.T) {
	r := newRig(t)
	status, _, body := r.do("POST", "/v1/tasks/t2/say", `{"message":"`+strings.Repeat("x", 2<<20)+`"}`)
	if status != 400 || !strings.Contains(body, `"exit_code":2`) {
		t.Errorf("an oversized body = %d %.100s", status, body)
	}
}

// ---- idempotency ----

func TestIdempotencyKeyReplaysTheStoredResponse(t *testing.T) {
	r := newRig(t)
	body := `{"issue_url":"https://github.com/wstein/workharbor/issues/7","agent":"docs-ws/docs"}`
	s1, h1, b1 := r.do("POST", "/v1/tasks", body, "Idempotency-Key", "k-1")
	s2, h2, b2 := r.do("POST", "/v1/tasks", body, "Idempotency-Key", "k-1")
	if s1 != 201 || s2 != 201 || b1 != b2 {
		t.Fatalf("first %d %q, second %d %q", s1, b1, s2, b2)
	}
	if h1.Get("Idempotent-Replay") != "" || h2.Get("Idempotent-Replay") != "true" {
		t.Errorf("replay headers: %q then %q", h1.Get("Idempotent-Replay"), h2.Get("Idempotent-Replay"))
	}
	if r.be.runs != 1 {
		t.Errorf("the task was started %d times, want once", r.be.runs)
	}
	// The same key with another request is a conflict.
	other := `{"issue_url":"https://github.com/wstein/workharbor/issues/8","agent":"docs-ws/docs"}`
	if s, _, b := r.do("POST", "/v1/tasks", other, "Idempotency-Key", "k-1"); s != 409 || !strings.Contains(b, "idempotency") {
		t.Errorf("a reused key = %d %s", s, b)
	}
	if r.be.runs != 1 {
		t.Errorf("a refused reuse ran the request: %d", r.be.runs)
	}
	// Without a key every request runs.
	r.do("POST", "/v1/tasks", body)
	r.do("POST", "/v1/tasks", body)
	if r.be.runs != 3 {
		t.Errorf("runs = %d, want 3", r.be.runs)
	}
	// Keys of different requests do not collide.
	if s, _, _ := r.do("POST", "/v1/tasks", other, "Idempotency-Key", "k-2"); s != 201 {
		t.Errorf("a second key = %d", s)
	}
}

func TestAFailedRequestStoresNothing(t *testing.T) {
	r := newRig(t)
	var fail atomic.Bool
	fail.Store(true)
	r.be.onCancel = func(domain.ID) error {
		if fail.Load() {
			return domain.NewConflict("test", "not now")
		}
		return nil
	}
	if s, _, _ := r.do("POST", "/v1/tasks/t2/cancel", "", "Idempotency-Key", "k-f"); s != 409 {
		t.Fatalf("first = %d", s)
	}
	fail.Store(false)
	s, h, _ := r.do("POST", "/v1/tasks/t2/cancel", "", "Idempotency-Key", "k-f")
	if s != 200 || h.Get("Idempotent-Replay") != "" {
		t.Errorf("the retry = %d (replay %q): a failure must not be stored", s, h.Get("Idempotent-Replay"))
	}
}

func TestTwoRequestsWithOneKeyRunOnce(t *testing.T) {
	r := newRig(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	r.be.onRun = func(service.RunRequest) (domain.ID, domain.ID, error) {
		if runs.Add(1) == 1 {
			close(started)
			<-release
		}
		return "t9", "r9", nil
	}
	body := `{"issue_url":"https://github.com/wstein/workharbor/issues/7","agent":"docs-ws/docs"}`
	var wg sync.WaitGroup
	res := make([]string, 2)
	wg.Add(1)
	go func() { defer wg.Done(); _, _, res[0] = r.do("POST", "/v1/tasks", body, "Idempotency-Key", "k-c") }()
	<-started
	wg.Add(1)
	go func() { defer wg.Done(); _, _, res[1] = r.do("POST", "/v1/tasks", body, "Idempotency-Key", "k-c") }()
	time.Sleep(100 * time.Millisecond) // the second request waits for the key
	close(release)
	wg.Wait()
	if runs.Load() != 1 || res[0] != res[1] {
		t.Errorf("runs = %d, responses %q and %q", runs.Load(), res[0], res[1])
	}
}

// ---- events ----

type sseEvent struct{ id, event, data string }

func readSSE(t *testing.T, r io.Reader, n int, timeout time.Duration) (events []sseEvent, comments []string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(r)
		var cur sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur.event != "" {
					events = append(events, cur)
					cur = sseEvent{}
					if len(events) == n {
						return
					}
				}
			case strings.HasPrefix(line, ":"):
				comments = append(comments, line)
			case strings.HasPrefix(line, "id: "):
				cur.id = line[4:]
			case strings.HasPrefix(line, "event: "):
				cur.event = line[7:]
			case strings.HasPrefix(line, "data: "):
				cur.data = line[6:]
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("timed out after %d of %d events", len(events), n)
	}
	return events, comments
}

func (r *rig) stream(path string, hdr ...string) (*http.Response, context.CancelFunc) {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", r.ts.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		r.t.Fatal(err)
	}
	return resp, cancel
}

func TestEventsAreServerSentWithIDsAndHeartbeats(t *testing.T) {
	r := newRig(t)
	r.be.events = make(chan domain.Event, 8)
	r.be.events <- domain.Event{Seq: 11, TaskID: "t2", Kind: domain.EventRunState, Tier: domain.TierAudit, At: t0, Payload: []byte(`{"to":"running"}`)}
	r.be.events <- domain.Event{TaskID: "t2", Kind: "token", Tier: domain.TierEphemeral, At: t0, Payload: []byte(`{"text":"he"}`)}
	resp, cancel := r.stream("/v1/tasks/t2/events", "Last-Event-ID", "10")
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	evs, comments := readSSE(t, resp.Body, 2, 5*time.Second)
	if evs[0].id != "11" || evs[0].event != "run.state" || !strings.Contains(evs[0].data, `"seq":11`) || !strings.Contains(evs[0].data, `"data":{"to":"running"}`) {
		t.Errorf("durable event = %+v", evs[0])
	}
	if evs[1].id != "" || evs[1].event != "token" || strings.Contains(evs[1].data, `"seq"`) {
		t.Errorf("ephemeral event = %+v: it must carry no id", evs[1])
	}
	if len(r.be.since) != 1 || r.be.since[0] != 10 {
		t.Errorf("Last-Event-ID reached the service as %v, want [10]", r.be.since)
	}
	// A heartbeat comes with nothing else going on.
	_, comments2 := readSSE(t, &timedReader{resp.Body}, 1, 5*time.Second)
	if len(comments)+len(comments2) == 0 {
		t.Error("no heartbeat comment")
	}
}

// timedReader returns what the stream gives, and gives up on its own once a
// heartbeat has been seen, because readSSE otherwise waits for an event.
type timedReader struct{ io.Reader }

func (t *timedReader) Read(p []byte) (int, error) {
	n, err := t.Reader.Read(p)
	if bytes.Contains(p[:n], []byte(": heartbeat")) {
		return n, io.EOF
	}
	return n, err
}

func TestEventsResumeFromSinceAndRefuseBadInput(t *testing.T) {
	r := newRig(t)
	resp, cancel := r.stream("/v1/tasks/t2/events?since=7")
	cancel()
	_ = resp.Body.Close()
	resp, cancel = r.stream("/v1/tasks/t2/events?since=7", "Last-Event-ID", "9") // the header wins
	cancel()
	_ = resp.Body.Close()
	if len(r.be.since) != 2 || r.be.since[0] != 7 || r.be.since[1] != 9 {
		t.Errorf("since = %v, want [7 9]", r.be.since)
	}
	for _, bad := range []string{"-1", "abc", "1.5"} {
		if s, _, _ := r.do("GET", "/v1/tasks/t2/events?since="+bad, ""); s != 400 {
			t.Errorf("since=%s = %d, want 400", bad, s)
		}
	}
	if s, _, b := r.do("GET", "/v1/tasks/nope/events", ""); s != 404 || !strings.Contains(b, `"ok":false`) {
		t.Errorf("an unknown task = %d %s: it must be an error envelope, not a stream", s, b)
	}
}

func TestAClosedSubscriptionEndsTheStream(t *testing.T) {
	r := newRig(t)
	r.be.events = make(chan domain.Event)
	resp, cancel := r.stream("/v1/tasks/t2/events")
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	close(r.be.events) // the service dropped the subscriber
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream stayed open after the subscription closed")
	}
}

func TestTheBackendIsComplete(t *testing.T) {
	var _ Backend = backend{}
	if got := Routes(); len(got) != 12 {
		sort.Strings(got)
		t.Errorf("routes = %v", got)
	}
}
