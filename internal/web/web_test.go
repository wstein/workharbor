package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/store"
)

const token = "s3cret-token-value"

var (
	bg = context.Background()
	t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
)

// fake is an api.Backend whose answers a test sets, and which records what it
// was asked.
type fake struct {
	mu        sync.Mutex
	runs      []service.RunRequest
	says      []string
	cancels   []domain.ID
	pauses    []domain.ID
	resumes   []domain.ID
	purges    []string
	answers   []domain.Response
	answerIDs []domain.ID
	since     []int64
	opens     []string
	events    chan domain.Event

	usage        func(service.UsageQuery) (service.UsageReport, error)
	usageQueries []service.UsageQuery

	tasks    []store.TaskSummary
	inbox    []domain.Decision
	logs     []domain.Event
	show     func(domain.ID) (service.TaskView, error)
	runRes   service.RunResult
	delivery agent.Delivery
	failSay  error
}

func (f *fake) List(context.Context, bool) ([]store.TaskSummary, error) { return f.tasks, nil }
func (f *fake) Inbox(context.Context) ([]domain.Decision, error)        { return f.inbox, nil }
func (f *fake) Show(_ context.Context, id domain.ID) (service.TaskView, error) {
	if f.show != nil {
		return f.show(id)
	}
	if id == "nope" {
		return service.TaskView{}, &domain.NotFoundError{Kind: "task", ID: "nope"}
	}
	return service.TaskView{Task: domain.Task{ID: id, Repo: "wstein/workharbor", Issue: "#7", State: domain.TaskRunning}, Runs: []domain.Run{{ID: "r1", State: domain.RunRunning}}, Agent: "docs/runtime"}, nil
}

func (f *fake) Run(_ context.Context, req service.RunRequest) (service.RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, req)
	if f.runRes.Task == "" {
		return service.RunResult{Task: "t9", Run: "r9"}, nil
	}
	return f.runRes, nil
}

func (f *fake) Say(_ context.Context, _ domain.ID, m string) (agent.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSay != nil {
		return "", f.failSay
	}
	f.says = append(f.says, m)
	if f.delivery == "" {
		return agent.DeliveryNextTurn, nil
	}
	return f.delivery, nil
}

func (f *fake) Cancel(_ context.Context, id domain.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
	return nil
}

func (f *fake) Pause(_ context.Context, id domain.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pauses = append(f.pauses, id)
	return nil
}

func (f *fake) Resume(_ context.Context, id domain.ID) (domain.ID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumes = append(f.resumes, id)
	return "r1", nil
}

func (f *fake) TranscriptSize(context.Context, domain.ID) (service.TranscriptSize, error) {
	return service.TranscriptSize{Events: 12, Bytes: 3400}, nil
}

func (f *fake) PurgeTranscript(_ context.Context, id domain.ID, actor string) (store.PurgeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purges = append(f.purges, string(id)+" by "+actor)
	return store.PurgeResult{Events: 12, Bytes: 3400, Digest: "abc123"}, nil
}

func (f *fake) Answer(_ context.Context, id domain.ID, r domain.Response) (domain.ID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answerIDs = append(f.answerIDs, id)
	f.answers = append(f.answers, r)
	return "", nil
}

func (f *fake) WorkspaceList(context.Context) ([]service.WorkspaceView, error) {
	return []service.WorkspaceView{{Workspace: domain.Workspace{ID: "w1", Name: "docs", Repo: "wstein/workharbor"}, Agents: []domain.Agent{{ID: "a1", Role: "runtime", Branch: "agent/runtime"}}}}, nil
}

func (f *fake) CreateWorkspace(context.Context, service.CreateRequest) (domain.Workspace, domain.Agent, error) {
	return domain.Workspace{}, domain.Agent{}, errors.New("not used")
}
func (f *fake) RemoveWorkspace(context.Context, string) error { return errors.New("not used") }
func (f *fake) AddAgent(context.Context, string, string, string, string) (domain.Agent, error) {
	return domain.Agent{}, errors.New("not used")
}
func (f *fake) RemoveAgent(context.Context, string, string) error { return errors.New("not used") }
func (f *fake) OpenCopy(_ context.Context, ws, role string) (service.EditorCopy, error) {
	f.mu.Lock()
	f.opens = append(f.opens, ws+"/"+role)
	f.mu.Unlock()
	return service.EditorCopy{Path: "/Users/whr/open/" + ws + "." + role, Warnings: []string{".vscode/tasks.json", "<b>x</b>"}}, nil
}

func (f *fake) KillAll(context.Context, string) (service.KillReport, error) {
	return service.KillReport{}, errors.New("not used")
}

func (f *fake) Usage(_ context.Context, q service.UsageQuery) (service.UsageReport, error) {
	f.mu.Lock()
	f.usageQueries = append(f.usageQueries, q)
	fn := f.usage
	f.mu.Unlock()
	if fn != nil {
		return fn(q)
	}
	return service.UsageReport{Rows: []service.UsageRow{}, Windows: []store.WindowReading{}}, nil
}

func (f *fake) Log(context.Context, domain.ID, int64, int) ([]domain.Event, error) {
	return f.logs, nil
}

func (f *fake) Subscribe(_ context.Context, _ domain.ID, since int64) (<-chan domain.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = append(f.since, since)
	if f.events == nil {
		f.events = make(chan domain.Event, 8)
	}
	return f.events, nil
}

func (f *fake) calls() (runs, cancels, answers int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs), len(f.cancels), len(f.answers)
}

type rig struct {
	t    *testing.T
	be   *fake
	auth *TokenAuth
	srv  *httptest.Server
	now  time.Time
	mu   sync.Mutex
}

func newRig(t *testing.T) *rig { return newRigWith(t) }

// newRigWith is newRig with the options of the UI changed.
func newRigWith(t *testing.T, more ...func(*Options)) *rig {
	t.Helper()
	st, err := store.Open(bg, filepath.Join(t.TempDir(), "workharbor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &rig{t: t, be: &fake{}, now: t0}
	r.auth, err = NewTokenAuth([]byte(token), func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now })
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{Auth: r.auth, Store: st, Heartbeat: 20 * time.Millisecond, Now: func() time.Time { return t0 }, OnError: func(err error) { t.Errorf("internal error: %v", err) }}
	for _, m := range more {
		m(&opt)
	}
	ui, err := New(r.be, opt)
	if err != nil {
		t.Fatal(err)
	}
	r.srv = httptest.NewServer(ui.Handler())
	t.Cleanup(r.srv.Close)
	return r
}

func (r *rig) advance(d time.Duration) { r.mu.Lock(); r.now = r.now.Add(d); r.mu.Unlock() }

// browser is a client with a cookie jar that does not follow redirects.
type browser struct {
	r  *rig
	c  *http.Client
	hd http.Header
}

func (r *rig) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{r: r, hd: http.Header{}, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// reply is what a request got back; its body is already read and closed.
type reply struct {
	StatusCode int
	Header     http.Header
	cookies    []*http.Cookie
}

func (r *reply) Cookies() []*http.Cookie { return r.cookies }

func (b *browser) do(method, path string, form url.Values) (*reply, string) {
	b.r.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(bg, method, b.r.srv.URL+path, body)
	if err != nil {
		b.r.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range b.hd {
		req.Header[k] = v
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.r.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return &reply{StatusCode: resp.StatusCode, Header: resp.Header, cookies: resp.Cookies()}, string(raw)
}

func (b *browser) signIn() {
	b.r.t.Helper()
	resp, _ := b.do("POST", "/login", url.Values{"token": {token}})
	if resp.StatusCode != http.StatusSeeOther {
		b.r.t.Fatalf("sign in: %d", resp.StatusCode)
	}
}

var (
	csrfRE = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)
	keyRE  = regexp.MustCompile(`name="key" value="([0-9a-f]+)"`)
)

// form loads a page and returns the CSRF token and the first idempotency key in it.
func (b *browser) form(path string) (csrf, key string) {
	b.r.t.Helper()
	resp, body := b.do("GET", path, nil)
	if resp.StatusCode != 200 {
		b.r.t.Fatalf("GET %s: %d\n%s", path, resp.StatusCode, body)
	}
	c := csrfRE.FindStringSubmatch(body)
	if c == nil {
		b.r.t.Fatalf("no CSRF token on %s", path)
	}
	if k := keyRE.FindStringSubmatch(body); k != nil {
		key = k[1]
	}
	return c[1], key
}

func TestNothingIsServedWithoutASession(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	for _, p := range []string{"/", "/inbox", "/tasks/t1", "/tasks/t1/cancel"} {
		resp, _ := b.do("GET", p, nil)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
			t.Errorf("GET %s: %d %s, want a redirect to /login", p, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	if resp, _ := b.do("GET", "/tasks/t1/events", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the stream without a session: %d, want 401", resp.StatusCode)
	}
	for _, p := range []string{"/tasks", "/decisions/d1/answer", "/logout"} {
		if resp, _ := b.do("POST", p, url.Values{"csrf": {"x"}}); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("POST %s without a session: %d", p, resp.StatusCode)
		}
	}
	// the sign-in page and the static files are public: the page needs its stylesheet
	if resp, body := b.do("GET", "/login", nil); resp.StatusCode != 200 || !strings.Contains(body, `name="token"`) {
		t.Errorf("login page: %d", resp.StatusCode)
	}
	if resp, body := b.do("GET", "/static/app.css", nil); resp.StatusCode != 200 || !strings.Contains(body, "--teal") {
		t.Errorf("css: %d", resp.StatusCode)
	}
	if resp, _ := b.do("GET", "/nope", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown page: %d", resp.StatusCode)
	}
}

func TestEveryPageCarriesTheStrictHeadersAndNoInlineScript(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	for _, p := range []string{"/login", "/", "/inbox", "/tasks/t1", "/tasks/t1/cancel", "/tasks/nope"} {
		resp, body := b.do("GET", p, nil)
		csp := resp.Header.Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "frame-ancestors 'none'", "form-action 'self'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: CSP %q lacks %q", p, csp, want)
			}
		}
		if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
			t.Errorf("%s: CSP allows inline code: %s", p, csp)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Referrer-Policy") != "no-referrer" || resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: headers %v", p, resp.Header)
		}
		// the only scripts are our own files, and nothing is styled or scripted inline
		for _, m := range regexp.MustCompile(`<script[^>]*>`).FindAllString(body, -1) {
			if !strings.Contains(m, `src="/static/`) {
				t.Errorf("%s: an inline script: %s", p, m)
			}
		}
		for _, bad := range []string{" style=", " onclick=", " onerror=", " onload=", "javascript:"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s: contains %q", p, bad)
			}
		}
	}
}

func TestSigningInNeedsTheTokenAndSetsAStrictCookie(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	resp, _ := b.do("POST", "/login", url.Values{"token": {"wrong"}})
	if resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 0 {
		t.Fatalf("a wrong token: %d, cookies %v", resp.StatusCode, resp.Cookies())
	}
	resp, _ = b.do("POST", "/login", url.Values{"token": {token + " "}}) // pasted with a trailing space
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("the token: %d", resp.StatusCode)
	}
	c := resp.Cookies()[0]
	if c.Name != cookieName || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure || c.Path != "/" || len(c.Value) < 60 {
		t.Errorf("cookie %+v over plain HTTP", c)
	}
	if strings.Contains(c.Value, token) {
		t.Error("the cookie holds the token")
	}
	// whether a request is https comes from public_url, never from what the request
	// says: a spoofed X-Forwarded-Proto on loopback is still plain http
	loginAs := func(host, xfp string) []*http.Cookie {
		req, _ := http.NewRequestWithContext(bg, "POST", r.srv.URL+"/login", strings.NewReader(url.Values{"token": {token}}.Encode()))
		if host != "" {
			req.Host = host
		}
		if xfp != "" {
			req.Header.Set("X-Forwarded-Proto", xfp)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := r.browser().c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.Cookies()
	}
	r.auth.SetPublicHost("whr.example.test")
	if cs := loginAs("", "https"); len(cs) != 1 || cs[0].Secure || cs[0].Name != cookieName {
		t.Errorf("a spoofed X-Forwarded-Proto on loopback: %+v", cs)
	}
	cs := loginAs("whr.example.test", "")
	if len(cs) != 1 || !cs[0].Secure || cs[0].Name != "__Host-"+cookieName || cs[0].Path != "/" || cs[0].Domain != "" {
		t.Fatalf("by the forwarder's name: %+v, want Secure, __Host- prefixed, Path=/ and no Domain", cs)
	}
	c = cs[0]
	// over https only the prefixed name is the session: the same value under the plain
	// name, as another port of this host name could toss it (a preview, D33), is not
	getInbox := func(cookie string) int {
		req, _ := http.NewRequestWithContext(bg, "GET", r.srv.URL+"/inbox", nil)
		req.Host = "whr.example.test"
		req.Header.Set("Cookie", cookie)
		resp, err := r.browser().c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := getInbox(c.Name + "=" + c.Value); got != http.StatusOK {
		t.Errorf("the issued cookie: %d", got)
	}
	if got := getInbox(cookieName + "=" + c.Value); got != http.StatusSeeOther {
		t.Errorf("a cookie under the plain name over https opened the session: %d", got)
	}
	// without public_url nothing is https
	r.auth.SetPublicHost("")
	if cs := loginAs("whr.example.test", "https"); len(cs) != 1 || cs[0].Secure {
		t.Errorf("without public_url: %+v", cs)
	}
	// a cross-site sign-in form is refused
	b3 := r.browser()
	b3.hd.Set("Origin", "https://evil.example")
	if resp, _ := b3.do("POST", "/login", url.Values{"token": {token}}); resp.StatusCode != http.StatusForbidden || len(resp.Cookies()) != 0 {
		t.Errorf("a cross-site sign-in: %d", resp.StatusCode)
	}
}

func TestFailedTriesAreLimitedAndASessionExpires(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	for range maxFailures {
		b.do("POST", "/login", url.Values{"token": {"wrong"}})
	}
	if resp, _ := b.do("POST", "/login", url.Values{"token": {token}}); resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("after %d failures even the right token gets %d, want 429", maxFailures, resp.StatusCode)
	}
	r.advance(failureWindow + time.Second)
	b.signIn()
	if resp, _ := b.do("GET", "/", nil); resp.StatusCode != 200 {
		t.Fatalf("signed in: %d", resp.StatusCode)
	}
	r.advance(sessionTTL + time.Second)
	if resp, _ := b.do("GET", "/", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("an expired session: %d, want a redirect to sign in", resp.StatusCode)
	}
}

func TestSigningOutEndsTheSession(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	csrf, _ := b.form("/")
	if resp, _ := b.do("POST", "/logout", url.Values{"csrf": {csrf}}); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if resp, _ := b.do("GET", "/", nil); resp.StatusCode != http.StatusSeeOther {
		t.Error("the session survived a sign-out")
	}
}

// Everything that came from an issue, an agent or the forge is text, escaped.
func TestUntrustedTextIsNeverHTML(t *testing.T) {
	r := newRig(t)
	evil := `<script>alert(1)</script><img src=x onerror=alert(2)>`
	r.be.tasks = []store.TaskSummary{{ID: "t1", Repo: "acme/" + evil, Issue: "#" + evil, State: domain.TaskAwaitingGuidance, Agent: evil}}
	r.be.inbox = []domain.Decision{{ID: "d1", TaskID: "t1", Kind: domain.DecisionApproval, Subject: evil, Input: evil, Options: []string{"allow", "deny"}}}
	payload, _ := json.Marshal(agent.Event{Kind: agent.EventMessage, Text: evil})
	call, _ := json.Marshal(agent.Event{Kind: agent.EventToolCall, Tool: evil, Input: evil})
	r.be.logs = []domain.Event{
		{Seq: 1, TaskID: "t1", Kind: domain.EventTranscript, Payload: payload, At: t0},
		{Seq: 2, TaskID: "t1", Kind: domain.EventTranscript, Payload: call, At: t0},
	}
	b := r.browser()
	b.signIn()
	for _, p := range []string{"/", "/inbox", "/tasks/t1"} {
		_, body := b.do("GET", p, nil)
		if strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") {
			t.Errorf("%s: untrusted text became HTML:\n%s", p, body)
		}
		if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("%s: the text is not shown escaped", p)
		}
	}
}

func TestNoTemplateOpensAnEscapeHatch(t *testing.T) {
	raw, err := os.ReadFile("views.templ")
	if err != nil {
		t.Fatal(err)
	}
	// the comment at the top says so in words; code must not use them
	code := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(string(raw), "")
	for _, bad := range []string{"templ.Raw", "templ.Unsafe", "SafeScript", "SafeCSS", "@templ.", "unsafe"} {
		if strings.Contains(code, bad) {
			t.Errorf("views.templ uses %q", bad)
		}
	}
}

func TestHarborPutsWhatNeedsYouFirst(t *testing.T) {
	r := newRig(t)
	r.be.tasks = []store.TaskSummary{
		{ID: "t1", Repo: "wstein/workharbor", Issue: "#1", State: domain.TaskRunning},
		{ID: "t2", Repo: "wstein/workharbor", Issue: "#2", State: domain.TaskAwaitingGuidance, Agent: "docs/runtime"},
	}
	r.be.inbox = []domain.Decision{{ID: "d1"}, {ID: "d2"}}
	b := r.browser()
	b.signIn()
	_, body := b.do("GET", "/", nil)
	needs, tasks := strings.Index(body, "Needs you"), strings.Index(body, ">Tasks<")
	if needs < 0 || tasks < 0 || needs > strings.Index(body, "wstein/workharbor#2") || strings.Index(body, "wstein/workharbor#2") > tasks || strings.Index(body, "wstein/workharbor#1") < tasks {
		t.Errorf("the task awaiting guidance is not first:\n%s", body)
	}
	if !strings.Contains(body, `<span class="count">2</span>`) {
		t.Error("the inbox count is missing")
	}
	if !strings.Contains(body, `<option value="docs/runtime">`) {
		t.Error("the agents are not offered to start a task")
	}
}

func TestEveryPostNeedsTheTokenAndASameSiteOrigin(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	csrf, key := b.form("/")
	good := func() url.Values {
		return url.Values{"csrf": {csrf}, "key": {key}, "issue": {"https://github.com/wstein/workharbor/issues/7"}, "agent": {"docs/runtime"}}
	}
	check := func(name string, mut func(url.Values), headers map[string]string, want int) {
		t.Helper()
		f := good()
		mut(f)
		bb := &browser{r: r, c: b.c, hd: http.Header{}}
		for k, v := range headers {
			bb.hd.Set(k, v)
		}
		if resp, _ := bb.do("POST", "/tasks", f); resp.StatusCode != want {
			t.Errorf("%s: %d, want %d", name, resp.StatusCode, want)
		}
	}
	check("no token", func(v url.Values) { v.Del("csrf") }, nil, http.StatusForbidden)
	check("a wrong token", func(v url.Values) { v.Set("csrf", strings.Repeat("0", 64)) }, nil, http.StatusForbidden)
	check("another site's origin", func(url.Values) {}, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden)
	check("a null origin", func(url.Values) {}, map[string]string{"Origin": "null"}, http.StatusForbidden)
	check("a cross-site fetch", func(url.Values) {}, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden)
	if runs, _, _ := r.be.calls(); runs != 0 {
		t.Fatalf("a refused request reached the service (%d runs)", runs)
	}
	check("the page's own request", func(url.Values) {}, map[string]string{"Origin": r.srv.URL, "Sec-Fetch-Site": "same-origin"}, http.StatusSeeOther)
}

// A double tap, or a retry, answers once: the repeat gets the same place to go.
func TestAWriteWithTheSameKeyRunsOnce(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	csrf, key := b.form("/")
	form := url.Values{"csrf": {csrf}, "key": {key}, "issue": {"https://github.com/wstein/workharbor/issues/7"}, "agent": {"docs/runtime"}}
	first, _ := b.do("POST", "/tasks", form)
	second, _ := b.do("POST", "/tasks", form)
	if first.StatusCode != http.StatusSeeOther || second.Header.Get("Location") != first.Header.Get("Location") || first.Header.Get("Location") != "/tasks/t9?flash=started" {
		t.Errorf("redirects %q then %q", first.Header.Get("Location"), second.Header.Get("Location"))
	}
	if runs, _, _ := r.be.calls(); runs != 1 {
		t.Errorf("the issue was started %d times, want once", runs)
	}
	if got := r.be.runs[0]; got.IssueURL != "https://github.com/wstein/workharbor/issues/7" || got.Agent != "docs/runtime" {
		t.Errorf("the service got %+v", got)
	}
	// without a key the form is refused: a retry could not be told from a new one
	form.Del("key")
	if resp, _ := b.do("POST", "/tasks", form); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("no key: %d, want 400", resp.StatusCode)
	}
	// a held issue goes to the inbox and says why
	r.be.runRes = service.RunResult{Task: "t4", Decision: "d4", Held: true}
	_, key2 := b.form("/")
	form.Set("key", key2)
	if resp, _ := b.do("POST", "/tasks", form); resp.Header.Get("Location") != "/inbox?flash=held" {
		t.Errorf("a held issue goes to %q", resp.Header.Get("Location"))
	}
	if _, body := b.do("GET", "/inbox?flash=held", nil); !strings.Contains(body, "do not trust") {
		t.Error("the inbox does not say why nothing started")
	}
}

func TestAnsweringADecision(t *testing.T) {
	r := newRig(t)
	r.be.inbox = []domain.Decision{
		{ID: "d1", TaskID: "t1", Kind: domain.DecisionApproval, Blocking: true, Subject: "Bash", Input: "make deploy", Options: []string{"allow", "deny"}},
		{ID: "d2", TaskID: "t1", Kind: domain.DecisionReview, Blocking: true, SHA: "aaa111", Subject: "Ready to push?", Options: []string{"allow", "deny"}},
		{ID: "d3", TaskID: "t1", Kind: domain.DecisionQuestion, Cause: domain.CauseAuthExpired, Options: []string{"resume", "cancel"}},
	}
	b := r.browser()
	b.signIn()
	_, inbox := b.do("GET", "/inbox", nil)
	if !strings.Contains(inbox, `name="option" value="allow"`) || !strings.Contains(inbox, `name="option" value="resume"`) {
		t.Errorf("the options are not offered:\n%s", inbox)
	}
	// the review has no button here: it needs the passkey of D45
	if strings.Contains(inbox, "/decisions/d2/answer") || !strings.Contains(inbox, "whr approve") {
		t.Error("the review decision can be answered from the page")
	}
	csrf, key := b.form("/inbox")
	post := func(id, option string) *reply {
		resp, _ := b.do("POST", "/decisions/"+id+"/answer", url.Values{"csrf": {csrf}, "key": {key + id}, "option": {option}, "reason": {" not here "}})
		return resp
	}
	if resp := post("d1", "allow"); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/inbox?flash=answered" {
		t.Fatalf("allow: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if r.be.answerIDs[0] != "d1" || r.be.answers[0].Option != "allow" || r.be.answers[0].By != "web" || r.be.answers[0].Reason != "not here" || !r.be.answers[0].At.Equal(t0) {
		t.Errorf("the service got %v %+v", r.be.answerIDs, r.be.answers)
	}
	post("d1", "allow") // the same key again: answered once
	if _, _, a := r.be.calls(); a != 1 {
		t.Errorf("answered %d times", a)
	}
	// refused, and never reaching the service
	if resp := post("d2", "allow"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a review: %d, want 403", resp.StatusCode)
	}
	if resp := post("d1", "merge"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an option the decision does not offer: %d, want 400", resp.StatusCode)
	}
	if resp := post("nope", "allow"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown decision: %d, want 404", resp.StatusCode)
	}
	if _, _, a := r.be.calls(); a != 1 {
		t.Errorf("a refused answer reached the service: %d answers", a)
	}
}

func TestTheTaskPageShowsTheTailAndResumesTheStreamAfterIt(t *testing.T) {
	r := newRig(t)
	for i := int64(1); i <= maxShown+5; i++ {
		p, _ := json.Marshal(agent.Event{Kind: agent.EventMessage, Text: "line " + itoa(i)})
		r.be.logs = append(r.be.logs, domain.Event{Seq: i, TaskID: "t1", Kind: domain.EventTranscript, Payload: p, At: t0})
	}
	b := r.browser()
	b.signIn()
	_, body := b.do("GET", "/tasks/t1", nil)
	if !strings.Contains(body, "5 earlier events") || strings.Contains(body, ">line 5<") || !strings.Contains(body, ">line 6<") || !strings.Contains(body, ">line 105<") {
		t.Errorf("the page does not show the last %d events:\n%s", maxShown, body)
	}
	if !strings.Contains(body, `sse-connect="/tasks/t1/events?since=105"`) || !strings.Contains(body, `sse-swap="ev"`) {
		t.Error("the live stream does not resume after the last event shown")
	}
	if resp, body := b.do("GET", "/tasks/nope", nil); resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "not found") {
		t.Errorf("an unknown task: %d", resp.StatusCode)
	}
}

func TestSendingAMessageAndCancelling(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	csrf, key := b.form("/tasks/t1")
	say := func(msg string, k string) *reply {
		resp, _ := b.do("POST", "/tasks/t1/say", url.Values{"csrf": {csrf}, "key": {k}, "message": {msg}})
		return resp
	}
	for delivery, flash := range map[agent.Delivery]string{agent.DeliveryInjected: "injected", agent.DeliveryNextTurn: "next_turn", agent.DeliveryResumedTurn: "resumed"} {
		r.be.delivery = delivery
		resp := say("look at the tests", key+string(delivery))
		if resp.Header.Get("Location") != "/tasks/t1?flash="+flash {
			t.Errorf("%s: redirected to %q", delivery, resp.Header.Get("Location"))
		}
	}
	if _, body := b.do("GET", "/tasks/t1?flash=resumed", nil); !strings.Contains(body, "resumed turn") {
		t.Error("the page does not say how the message was delivered")
	}
	if _, body := b.do("GET", "/tasks/t1?flash=<b>x</b>", nil); strings.Contains(body, "<b>x</b>") {
		t.Error("the flash took text from the query string")
	}
	if resp := say("   ", key+"empty"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an empty message: %d", resp.StatusCode)
	}
	r.be.failSay = &domain.ConflictError{Rule: "x", Msg: "task t1 has no live run"}
	if resp, body := b.do("POST", "/tasks/t1/say", url.Values{"csrf": {csrf}, "key": {key + "fail"}, "message": {"hi"}}); resp.StatusCode != http.StatusConflict || !strings.Contains(body, "no live run") {
		t.Errorf("a refused message: %d", resp.StatusCode)
	}

	// cancelling asks first
	if _, body := b.do("GET", "/tasks/t1/cancel", nil); !strings.Contains(body, "cannot be undone") {
		t.Error("the cancel page does not warn")
	}
	if _, c, _ := r.be.calls(); c != 0 {
		t.Fatal("opening the confirmation cancelled the task")
	}
	form := url.Values{"csrf": {csrf}, "key": {key + "cancel"}}
	b.do("POST", "/tasks/t1/cancel", form)
	b.do("POST", "/tasks/t1/cancel", form)
	if _, c, _ := r.be.calls(); c != 1 {
		t.Errorf("cancelled %d times, want once", c)
	}
}

func readStream(t *testing.T, resp *http.Response, want int) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := resp.Body.Read(buf)
		out.Write(buf[:n])
		if strings.Count(out.String(), "event: ev") >= want || err != nil {
			break
		}
	}
	return out.String()
}

func TestTheLiveStreamSendsEscapedRowsAndResumesFromTheLastID(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	r.be.events = make(chan domain.Event, 4)
	p, _ := json.Marshal(agent.Event{Kind: agent.EventMessage, Text: "<script>alert(1)</script>\nsecond line"})
	r.be.events <- domain.Event{Seq: 42, TaskID: "t1", Kind: domain.EventTranscript, Payload: p, At: t0}
	r.be.events <- domain.Event{TaskID: "t1", Kind: "token", Tier: domain.TierEphemeral, Payload: []byte(`{"text":"he"}`), At: t0}

	req, _ := http.NewRequestWithContext(bg, "GET", r.srv.URL+"/tasks/t1/events", nil)
	req.Header.Set("Last-Event-ID", "41")
	for _, c := range b.c.Jar.Cookies(&url.URL{Scheme: "http", Host: strings.TrimPrefix(r.srv.URL, "http://")}) {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{}).Do(req) //nolint:bodyclose // readStream closes it
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	got := readStream(t, resp, 2)
	if r.be.since[0] != 41 {
		t.Errorf("subscribed since %d, want the Last-Event-ID 41", r.be.since[0])
	}
	for _, want := range []string{": connected", "id: 42\nevent: ev\ndata: <li ", "&lt;script&gt;alert(1)&lt;/script&gt;"} {
		if !strings.Contains(got, want) {
			t.Errorf("the stream lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<script>") {
		t.Errorf("untrusted text reached the stream as HTML:\n%s", got)
	}
	if n := strings.Count(got, "id: "); n != 1 {
		t.Errorf("%d ids: the ephemeral event must carry none\n%s", n, got)
	}
	for _, line := range strings.Split(got, "\n") { // a data field never holds a newline
		if line != "" && !strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "id: ") && !strings.HasPrefix(line, "event: ") && !strings.HasPrefix(line, ":") {
			t.Errorf("a stray line in the stream: %q", line)
		}
	}
	if resp, _ := b.do("GET", "/tasks/t1/events?since=abc", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a bad since: %d", resp.StatusCode)
	}
}

func TestAnUnknownTaskHasNoStream(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	if resp, _ := b.do("GET", "/tasks/nope/events", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("%d, want 404", resp.StatusCode)
	}
}

// The vendored files are served as they were pinned.
func TestTheVendoredFilesMatchTheirChecksums(t *testing.T) {
	notes, err := os.ReadFile("static/VENDORED.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"htmx.min.js", "sse.js"} {
		raw, err := os.ReadFile(filepath.Join("static", name)) //nolint:gosec // a fixed file name of this test
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if !strings.Contains(string(notes), hex.EncodeToString(sum[:])) {
			t.Errorf("static/%s does not match the checksum in VENDORED.md", name)
		}
	}
}

// The editor copy is the supervisor's, made for the task's agent; the page lists
// the files an editor may run, as text.
func TestTheEditorCopyIsTheSupervisorsAndItsWarningsAreText(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	b.signIn()
	csrf, _ := b.form("/tasks/t1")
	_, task := b.do("GET", "/tasks/t1", nil)
	if !strings.Contains(task, `action="/tasks/t1/open"`) {
		t.Error("the task page offers no editor copy")
	}
	resp, body := b.do("POST", "/tasks/t1/open", url.Values{"csrf": {csrf}})
	if resp.StatusCode != 200 || len(r.be.opens) != 1 || r.be.opens[0] != "docs/runtime" {
		t.Fatalf("%d, opens %v", resp.StatusCode, r.be.opens)
	}
	for _, want := range []string{"/Users/whr/open/docs.runtime", ".vscode/tasks.json", "&lt;b&gt;x&lt;/b&gt;", "not the agent's checkout"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<b>x</b>") {
		t.Error("a file name became HTML")
	}
	if resp, _ := b.do("POST", "/tasks/t1/open", url.Values{}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("without the CSRF token: %d", resp.StatusCode)
	}
}
