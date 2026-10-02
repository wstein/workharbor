package preview

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

var bg = context.Background()

// rig is a manager over a fake runtime: one environment with one app.
type rig struct {
	t      *testing.T
	m      *Manager
	app    *httptest.Server
	mu     sync.Mutex
	dials  [][2]string // env and port of every dial
	run    map[string]bool
	now    time.Time
	closed []string
	seenIn chan *http.Request
}

func (r *rig) DialPreview(ctx context.Context, env string, port int) (net.Conn, error) {
	r.mu.Lock()
	r.dials = append(r.dials, [2]string{env, fmt.Sprint(port)})
	r.mu.Unlock()
	if env != "env1" || port != 3000 {
		return nil, errors.New("fake runtime: nothing listens there")
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", r.app.Listener.Addr().String())
}

func (r *rig) setRunning(env string, up bool) {
	r.mu.Lock()
	r.run[env] = up
	r.mu.Unlock()
}

func newRig(t *testing.T, mod ...func(*Config)) *rig {
	t.Helper()
	r := &rig{t: t, run: map[string]bool{"env1": true, "env2": true}, now: time.Unix(1_700_000_000, 0), seenIn: make(chan *http.Request, 16)}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, q *http.Request) {
		select {
		case r.seenIn <- q.Clone(context.Background()):
		default:
		}
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("Clear-Site-Data", `"cookies"`)
		http.SetCookie(w, &http.Cookie{Name: "app", Value: "1"})               //nolint:gosec // an app's cookie
		http.SetCookie(w, &http.Cookie{Name: "whr_session", Value: "forged"})  //nolint:gosec // an app's cookie
		http.SetCookie(w, &http.Cookie{Name: "__Host-whr_x", Value: "forged"}) //nolint:gosec // an app's cookie, as the test needs it
		_, _ = io.WriteString(w, "hello from the app: "+q.URL.RequestURI())    //nolint:gosec // a test app that is not a browser page
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		c, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		sc := bufio.NewScanner(rw)
		for sc.Scan() {
			_, _ = rw.WriteString("echo " + sc.Text() + "\n")
			_ = rw.Flush()
		}
	})
	r.app = httptest.NewServer(mux)
	t.Cleanup(r.app.Close)
	cfg := Config{
		Upstream: r,
		Live: func(_ context.Context, env string) bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.run[env]
		},
		Now:      func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		OnClosed: func(p Preview, why string) { r.mu.Lock(); r.closed = append(r.closed, p.ID+":"+why); r.mu.Unlock() },
		OnError:  func(err error) { t.Logf("preview error: %v", err) },
	}
	for _, m := range mod {
		m(&cfg)
	}
	var err error
	if r.m, err = New(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, p := range r.m.List() {
			r.m.Close(p.ID, "test over")
		}
	})
	return r
}

// dial opens a TCP connection to a preview's listener.
func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(bg, "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (r *rig) advance(d time.Duration) { r.mu.Lock(); r.now = r.now.Add(d); r.mu.Unlock() }

func (r *rig) open() Preview {
	r.t.Helper()
	p, _, err := r.m.Open(bg, "t1", "env1", 3000)
	if err != nil {
		r.t.Fatal(err)
	}
	return p
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// answer is what a test needs of a response, with its body already closed.
type answer struct {
	StatusCode int
	Header     http.Header
	cookies    []*http.Cookie
}

func (a answer) Cookies() []*http.Cookie { return a.cookies }

func get(t *testing.T, link string, cookies ...*http.Cookie) (answer, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(bg, "GET", link, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return answer{StatusCode: resp.StatusCode, Header: resp.Header, cookies: resp.Cookies()}, string(b)
}

// enter follows a preview's link and returns the cookie it set.
func (r *rig) enter(p Preview) (*http.Cookie, string) {
	r.t.Helper()
	link, err := r.m.Grant(p.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	resp, _ := get(r.t, link)
	if resp.StatusCode != http.StatusSeeOther || strings.Contains(resp.Header.Get("Location"), "whr_preview") {
		r.t.Fatalf("grant: %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	cs := resp.Cookies()
	if len(cs) != 1 || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode || !strings.HasPrefix(cs[0].Name, "whr_preview_") {
		r.t.Fatalf("cookies = %+v", cs)
	}
	return cs[0], link
}

func base(link string) string {
	u, _ := url.Parse(link)
	return u.Scheme + "://" + u.Host
}

func TestAPreviewNeedsItsGrantThenItsCookie(t *testing.T) {
	r := newRig(t)
	p := r.open()
	link, _ := r.m.Grant(p.ID)
	b := base(link)

	if resp, _ := get(t, b+"/"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no cookie: %d", resp.StatusCode)
	}
	wrong := &http.Cookie{Name: "whr_preview_" + strings.TrimPrefix(p.ID, "pv-"), Value: "guess"} //nolint:gosec // a request cookie
	if resp, _ := get(t, b+"/", wrong); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a wrong cookie: %d", resp.StatusCode)
	}
	if resp, _ := get(t, b+"/?whr_preview=guess"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a wrong grant: %d", resp.StatusCode)
	}

	resp, _ := get(t, link)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("grant: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookie := resp.Cookies()[0]
	// a grant works once
	if resp, _ := get(t, link); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a used grant: %d", resp.StatusCode)
	}
	// the app answers with the cookie, sees its own cookie and none of whr's, and
	// cannot set whr's
	uiSession := &http.Cookie{Name: "whr_session", Value: "the UI's session"} //nolint:gosec // a request cookie
	appCookie := &http.Cookie{Name: "app", Value: "mine"}                     //nolint:gosec // a request cookie
	resp, body := get(t, b+"/page?x=1", cookie, uiSession, appCookie)
	if resp.StatusCode != 200 || body != "hello from the app: /page?x=1" {
		t.Fatalf("through the proxy: %d %q", resp.StatusCode, body)
	}
	q := <-r.seenIn
	if q.Host != "localhost:3000" || q.Header.Get("X-Forwarded-Host") == "" {
		t.Errorf("app saw host %q, forwarded host %q", q.Host, q.Header.Get("X-Forwarded-Host"))
	}
	if cs := q.Cookies(); len(cs) != 1 || cs[0].Name != "app" {
		t.Errorf("the app saw cookies %+v", cs)
	}
	var names []string
	for _, c := range resp.Cookies() {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "app" {
		t.Errorf("the app's Set-Cookie reached the browser as %v, want only app", names)
	}
	if resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", resp.Header.Get("Referrer-Policy"))
	}
	if r.m.List()[0].Listen == 0 {
		t.Error("no listen port")
	}
}

func TestAGrantExpires(t *testing.T) {
	r := newRig(t)
	p := r.open()
	link, _ := r.m.Grant(p.ID)
	r.advance(GrantTTL + time.Second)
	if resp, _ := get(t, link); resp.StatusCode != http.StatusForbidden {
		t.Errorf("an expired grant: %d", resp.StatusCode)
	}
}

func TestOnlyTheOpenedEnvironmentAndPortAreEverDialled(t *testing.T) {
	r := newRig(t)
	p := r.open()
	cookie, link := r.enter(p)
	b := base(link)
	u, _ := url.Parse(b)

	// an absolute URI, another port, another host in the request line, and a Host
	// of someone else: the proxy dials its one pair or refuses
	raw := func(req string) string {
		c := dial(t, u.Host)
		defer func() { _ = c.Close() }()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = io.WriteString(c, req)
		line, _ := bufio.NewReader(c).ReadString('\n')
		return line
	}
	ck := "Cookie: " + cookie.Name + "=" + cookie.Value + "\r\n"
	for _, req := range []string{
		"GET http://evil.example:9999/x HTTP/1.1\r\nHost: " + u.Host + "\r\n" + ck + "Connection: close\r\n\r\n",
		"GET http://127.0.0.1:22/ HTTP/1.1\r\nHost: " + u.Host + "\r\n" + ck + "Connection: close\r\n\r\n",
		"GET http://env2:3000/ HTTP/1.1\r\nHost: env2:3000\r\n" + ck + "Connection: close\r\n\r\n",
	} {
		line := raw(req)
		if !strings.Contains(line, "200") && !strings.Contains(line, "421") {
			t.Errorf("%q answered %q", strings.SplitN(req, "\r\n", 2)[0], line)
		}
	}
	if resp, _ := get(t, b+"/"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no cookie: %d", resp.StatusCode)
	}
	req, _ := http.NewRequestWithContext(bg, "GET", b+"/", nil)
	req.Host = "attacker.example"
	req.AddCookie(cookie)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("another Host: %d, want 421", resp.StatusCode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.dials {
		if d != [2]string{"env1", "3000"} {
			t.Errorf("dialled %v, want only env1:3000", d)
		}
	}
	if len(r.dials) == 0 {
		t.Error("nothing was dialled: the test proved nothing")
	}
}

func TestAStoppedEnvironmentsPreviewRefusesAndCloses(t *testing.T) {
	r := newRig(t)
	p := r.open()
	cookie, link := r.enter(p)
	b := base(link)
	if resp, _ := get(t, b+"/", cookie); resp.StatusCode != 200 {
		t.Fatalf("while it runs: %d", resp.StatusCode)
	}
	r.setRunning("env1", false)
	r.advance(liveFor + time.Second) // the check is cached for a moment
	if resp, _ := get(t, b+"/", cookie); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a stopped environment's preview: %d, want 503", resp.StatusCode)
	}
	for i := 0; i < 100 && len(r.m.List()) != 0; i++ {
		time.Sleep(10 * time.Millisecond) // it closes just after the answer
	}
	if len(r.m.List()) != 0 {
		t.Errorf("the preview stayed open: %+v", r.m.List())
	}
	if _, _, err := r.m.Open(bg, "t1", "env1", 3000); !errors.Is(err, ErrNotRunning) {
		t.Errorf("open on a stopped environment: %v", err)
	}
	if c, err := (&net.Dialer{Timeout: time.Second}).DialContext(bg, "tcp", strings.TrimPrefix(b, "http://")); err == nil {
		_ = c.Close()
		t.Error("the listener is still open")
	}
}

func TestASweepClosesWhatTheEnvironmentLeftAndWhatGrewOld(t *testing.T) {
	r := newRig(t)
	p := r.open()
	q, _, _ := r.m.Open(bg, "t2", "env2", 3000)
	r.setRunning("env1", false)
	r.advance(liveFor + time.Second)
	r.m.Sweep(bg)
	if got := r.m.List(); len(got) != 1 || got[0].ID != q.ID {
		t.Fatalf("after the environment stopped: %+v (opened %s)", got, p.ID)
	}
	r.advance(DefaultMaxAge)
	r.m.Sweep(bg)
	if len(r.m.List()) != 0 {
		t.Errorf("an old preview stayed: %+v", r.m.List())
	}
	if len(r.closed) != 2 || !strings.HasSuffix(r.closed[0], "environment stopped") || !strings.HasSuffix(r.closed[1], "expired") {
		t.Errorf("closed = %v", r.closed)
	}
}

func TestAWebSocketPassesAndEndsWithThePreview(t *testing.T) {
	r := newRig(t)
	p := r.open()
	cookie, link := r.enter(p)
	u, _ := url.Parse(base(link))
	c := dial(t, u.Host)
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "GET /ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nCookie: %s=%s\r\n\r\n", u.Host, cookie.Name, cookie.Value)
	br := bufio.NewReader(c)
	status, _ := br.ReadString('\n')
	if !strings.Contains(status, "101") {
		t.Fatalf("upgrade: %q", status)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	_, _ = io.WriteString(c, "hot reload\n")
	if got, _ := br.ReadString('\n'); got != "echo hot reload\n" {
		t.Fatalf("through the socket: %q", got)
	}
	r.m.Close(p.ID, "closed by the human")
	if _, err := br.ReadString('\n'); err == nil {
		t.Error("the WebSocket survived its preview")
	}
}

func TestPortsComeFromTheRangeAndOneEnvironmentPortHasOnePreview(t *testing.T) {
	// two free ports for the test: take them from the kernel and release them
	var free [2]int
	for i := range free {
		l, err := (&net.ListenConfig{}).Listen(bg, "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		free[i] = l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
	}
	lo, hi := free[0], free[1]
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi-lo > 50 {
		t.Skip("no two ports close together")
	}
	r := newRig(t, func(c *Config) { c.FirstPort, c.LastPort = lo, hi })
	a, _, err := r.m.Open(bg, "t1", "env1", 3000)
	if err != nil {
		t.Fatal(err)
	}
	if a.Listen < lo || a.Listen > hi {
		t.Errorf("listening on %d, outside %d-%d", a.Listen, lo, hi)
	}
	if again, created, _ := r.m.Open(bg, "t1", "env1", 3000); again.ID != a.ID || created {
		t.Errorf("the same port of the same environment opened a second preview")
	}
	if _, _, err := r.m.Open(bg, "t1", "env1", 3001); err != nil && !errors.Is(err, ErrNoPort) {
		t.Fatal(err)
	}
	for port := 4000; ; port++ {
		if _, _, err := r.m.Open(bg, "t1", "env1", port); err != nil {
			if !errors.Is(err, ErrNoPort) {
				t.Fatal(err)
			}
			break
		}
		if port > 4000+hi-lo+2 {
			t.Fatal("the range never ran out")
		}
	}
	if _, _, err := r.m.Open(bg, "t1", "env1", 70000); !errors.Is(err, ErrBadPort) {
		t.Errorf("a bad port: %v", err)
	}
}

func TestClosingEndsTheTokensAndLinks(t *testing.T) {
	r := newRig(t)
	p := r.open()
	cookie, link := r.enter(p)
	b := base(link)
	r.m.CloseEnv("env1", "environment stopped")
	if _, err := r.m.Grant(p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a grant for a closed preview: %v", err)
	}
	if c, err := (&net.Dialer{Timeout: time.Second}).DialContext(bg, "tcp", strings.TrimPrefix(b, "http://")); err == nil {
		_ = c.Close()
		t.Error("the listener is open after a close")
	}
	_ = cookie
	r.m.Close(p.ID, "again") // closing twice is not an error
	if len(r.closed) != 1 {
		t.Errorf("closed = %v", r.closed)
	}
}

func TestTheLinkUsesTheForwardersNameAndHttps(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Scheme, c.Host = "https", "whr.example.test" })
	p := r.open()
	link, _ := r.m.Grant(p.ID)
	if !strings.HasPrefix(link, "https://whr.example.test:") || !strings.Contains(link, fmt.Sprint(p.Listen)) {
		t.Errorf("link = %s", link)
	}
	// reached through the forwarder's name, the cookie is Secure
	u, _ := url.Parse(link)
	req, _ := http.NewRequestWithContext(bg, "GET", "http://127.0.0.1:"+fmt.Sprint(p.Listen)+"/?"+u.RawQuery, nil)
	req.Host = "whr.example.test"
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) != 1 || !resp.Cookies()[0].Secure {
		t.Errorf("exchange via the forwarder's name: %d %+v", resp.StatusCode, resp.Cookies())
	}
}

// Over https the cookie carries the __Host- prefix, and a cookie the server did not
// issue, whatever its name or value, opens nothing.
func TestTheCookieIsHostPrefixedOverHttpsAndOthersAreIgnored(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Scheme, c.Host = "https", "whr.example.test" })
	p := r.open()
	link, _ := r.m.Grant(p.ID)
	u, _ := url.Parse(link)
	req, _ := http.NewRequestWithContext(bg, "GET", "http://127.0.0.1:"+fmt.Sprint(p.Listen)+"/?"+u.RawQuery, nil)
	req.Host = "whr.example.test"
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	cs := resp.Cookies()
	if len(cs) != 1 || !strings.HasPrefix(cs[0].Name, "__Host-whr_preview_") || !cs[0].Secure || cs[0].Path != "/" || cs[0].Domain != "" {
		t.Fatalf("cookies = %+v", cs)
	}
	base := "http://127.0.0.1:" + fmt.Sprint(p.Listen) + "/"
	try := func(c *http.Cookie) int {
		q, _ := http.NewRequestWithContext(bg, "GET", base, nil)
		q.Host = "whr.example.test"
		q.AddCookie(c)
		a, err := noRedirect.Do(q)
		if err != nil {
			t.Fatal(err)
		}
		_ = a.Body.Close()
		return a.StatusCode
	}
	if got := try(cs[0]); got != 200 {
		t.Errorf("the issued cookie: %d", got)
	}
	plain := &http.Cookie{Name: strings.TrimPrefix(cs[0].Name, "__Host-"), Value: cs[0].Value} //nolint:gosec // a request cookie
	if got := try(plain); got != http.StatusUnauthorized {
		t.Errorf("the right value under a name the server did not issue: %d", got)
	}
	forged := &http.Cookie{Name: cs[0].Name, Value: "forged"} //nolint:gosec // a request cookie
	if got := try(forged); got != http.StatusUnauthorized {
		t.Errorf("a cookie the server did not issue: %d", got)
	}
}

// A port is reused, so a preview may not register a service worker, the grant
// exchange clears the origin's storage and cache (never its cookies), and the app
// may neither pin the origin to https nor wipe what the UI keeps.
func TestAReusedOriginStartsCleanAndCannotKeepAServiceWorker(t *testing.T) {
	r := newRig(t)
	p := r.open()
	link, _ := r.m.Grant(p.ID)
	resp, _ := get(t, link)
	if got := resp.Header.Get("Clear-Site-Data"); got != `"storage", "cache"` {
		t.Errorf("Clear-Site-Data on the exchange = %q, want storage and cache only (never cookies)", got)
	}
	cookie := resp.Cookies()[0]
	b := base(link)
	req, _ := http.NewRequestWithContext(bg, "GET", b+"/sw.js", nil)
	req.Header.Set("Service-Worker", "script")
	req.AddCookie(cookie)
	sw, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = sw.Body.Close()
	if sw.StatusCode != http.StatusForbidden {
		t.Errorf("a service worker script: %d, want 403", sw.StatusCode)
	}
	ans, _ := get(t, b+"/", cookie)
	if ans.Header.Get("Strict-Transport-Security") != "" || ans.Header.Get("Clear-Site-Data") != "" {
		t.Errorf("the app's HSTS or Clear-Site-Data reached the browser: %v", ans.Header)
	}
}
