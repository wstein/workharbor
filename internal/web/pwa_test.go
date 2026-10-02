package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

const (
	iphone = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
	ipad   = "Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
)

// The manifest, the service worker and the offline page are fetched by the browser
// without a session, hold nothing private, and the manifest's icons resolve.
func TestTheInstallableAppFilesAreServedWithoutASession(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	resp, body := b.do("GET", "/manifest.webmanifest", nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/manifest+json") {
		t.Fatalf("manifest: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var m struct {
		StartURL string `json:"start_url"`
		Scope    string `json:"scope"`
		Display  string `json:"display"`
		Icons    []struct{ Src, Sizes, Type string }
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	if m.Display != "standalone" || m.StartURL != "/" || m.Scope != "/" || len(m.Icons) < 2 {
		t.Errorf("manifest = %+v", m)
	}
	for _, ic := range m.Icons {
		if resp, _ := b.do("GET", ic.Src, nil); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
			t.Errorf("icon %s: %d %q", ic.Src, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	resp, _ = b.do("GET", "/sw.js", nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("sw.js: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"))
	}
	if resp, body := b.do("GET", "/offline", nil); resp.StatusCode != 200 || !strings.Contains(body, "not reachable") {
		t.Errorf("offline: %d", resp.StatusCode)
	}
	// the login page links them, and the CSP lets the browser read the manifest
	resp, page := b.do("GET", "/login", nil)
	for _, want := range []string{`rel="manifest" href="/manifest.webmanifest"`, `rel="apple-touch-icon"`, `/static/pwa.js`} {
		if !strings.Contains(page, want) {
			t.Errorf("login page lacks %s", want)
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "manifest-src 'self'") {
		t.Errorf("CSP = %s", resp.Header.Get("Content-Security-Policy"))
	}
}

// The service worker keeps the app shell only: every cached path is a static file
// or the offline page, and it has no code path that stores a page or a response of
// another path (design §9.3: never diffs, transcripts or Decisions).
func TestTheServiceWorkerCachesOnlyTheAppShell(t *testing.T) {
	r := newRig(t)
	b := r.browser()
	_, js := b.do("GET", "/sw.js", nil)
	list := regexp.MustCompile(`(?s)const SHELL = \[(.*?)\];`).FindStringSubmatch(js)
	if list == nil {
		t.Fatal("no SHELL list in sw.js")
	}
	paths := regexp.MustCompile(`"(/[^"]*)"`).FindAllStringSubmatch(list[1], -1)
	if len(paths) < 5 {
		t.Fatalf("shell = %v", paths)
	}
	for _, p := range paths {
		path := p[1]
		if path != "/offline" && !strings.HasPrefix(path, "/static/") {
			t.Errorf("%s is cached but is not a static file", path)
		}
		if resp, _ := b.do("GET", path, nil); resp.StatusCode != 200 {
			t.Errorf("shell file %s: %d without a session", path, resp.StatusCode)
		}
	}
	// the only cache write is guarded by the shell list, and navigations go to the network
	if strings.Count(js, "c.put(") != 1 || !strings.Contains(js, "SHELL.indexOf(url.pathname) === -1") {
		t.Error("the worker stores something besides the shell")
	}
	if !strings.Contains(js, `req.method !== "GET"`) || !strings.Contains(js, "fetch(req).catch") {
		t.Error("the worker handles writes, or does not go to the network for pages")
	}
	for _, bad := range []string{"/tasks", "/inbox", "/events", "/decisions"} {
		if strings.Contains(list[1], bad) {
			t.Errorf("the shell lists %s", bad)
		}
	}
}

func (b *browser) asDevice(ua string) *browser {
	b.hd.Set("User-Agent", ua)
	return b
}

// Each browser has its own session, listed with a label from fixed words; another
// device ends one, and that device is signed out at once.
func TestADeviceIsListedAndSignedOutFromAnother(t *testing.T) {
	r := newRig(t)
	phone, tablet := r.browser().asDevice(iphone), r.browser().asDevice(ipad)
	phone.signIn()
	r.advance(time.Minute)
	tablet.signIn()

	resp, page := tablet.do("GET", "/devices", nil)
	if resp.StatusCode != 200 || !strings.Contains(page, "Phone, Safari") || !strings.Contains(page, "Tablet, Safari") || strings.Count(page, `"count">this device`) != 1 {
		t.Fatalf("devices: %d\n%s", resp.StatusCode, page)
	}
	revoke := regexp.MustCompile(`action="/devices/([0-9a-f]+)/revoke"`).FindAllStringSubmatch(page, -1)
	if len(revoke) != 2 {
		t.Fatalf("revoke forms: %v", revoke)
	}
	csrf := csrfOf(t, tablet)
	// without the CSRF token nothing happens
	if resp, _ := tablet.do("POST", "/devices/"+revoke[0][1]+"/revoke", url.Values{}); resp.StatusCode == http.StatusSeeOther {
		t.Error("a revoke without a CSRF token was accepted")
	}
	// the newest is the tablet itself, so the phone is the other one
	other := revoke[1][1]
	if resp, _ := tablet.do("POST", "/devices/"+other+"/revoke", url.Values{"csrf": {csrf}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	if resp, _ := phone.do("GET", "/inbox", nil); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Errorf("the revoked phone still has a session: %d", resp.StatusCode)
	}
	if resp, _ := tablet.do("GET", "/inbox", nil); resp.StatusCode != 200 {
		t.Errorf("the tablet lost its own session: %d", resp.StatusCode)
	}
	// revoking it again is harmless; an unknown ID is too
	if resp, _ := tablet.do("POST", "/devices/"+other+"/revoke", url.Values{"csrf": {csrf}}); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("a second revoke: %d", resp.StatusCode)
	}
	// ending the current device is a sign-out
	_, page = tablet.do("GET", "/devices", nil)
	cur := regexp.MustCompile(`action="/devices/([0-9a-f]+)/revoke"`).FindStringSubmatch(page)
	resp, _ = tablet.do("POST", "/devices/"+cur[1]+"/revoke", url.Values{"csrf": {csrf}})
	if resp.Header.Get("Location") != "/login" {
		t.Errorf("revoking itself went to %q", resp.Header.Get("Location"))
	}
	if resp, _ := tablet.do("GET", "/inbox", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("still signed in: %d", resp.StatusCode)
	}
}

// A phone signs itself out after a short idle time; use keeps it signed in, and a
// tablet or computer waits much longer.
func TestAPhoneSessionEndsAfterAShortIdleTime(t *testing.T) {
	r := newRig(t)
	phone, desk := r.browser().asDevice(iphone), r.browser().asDevice("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Gecko/20100101 Firefox/130.0")
	phone.signIn()
	desk.signIn()
	for range 4 { // ten minutes at a time: always inside the idle time
		r.advance(10 * time.Minute)
		if resp, _ := phone.do("GET", "/inbox", nil); resp.StatusCode != 200 {
			t.Fatalf("an active phone was signed out: %d", resp.StatusCode)
		}
	}
	r.advance(16 * time.Minute)
	if resp, _ := phone.do("GET", "/inbox", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("an idle phone kept its session: %d", resp.StatusCode)
	}
	// the computer was idle for the same time and is still in (12 hours is the limit)
	if resp, _ := desk.do("GET", "/inbox", nil); resp.StatusCode != 200 {
		t.Errorf("an idle computer was signed out after %v: %d", 16*time.Minute+40*time.Minute, resp.StatusCode)
	}
}

// openStream opens the live stream as a signed-in browser would.
func (b *browser) openStream() *http.Response {
	b.r.t.Helper()
	req, _ := http.NewRequestWithContext(bg, "GET", b.r.srv.URL+"/tasks/t1/events", nil)
	for k, v := range b.hd {
		req.Header[k] = v
	}
	for _, c := range b.c.Jar.Cookies(&url.URL{Scheme: "http", Host: strings.TrimPrefix(b.r.srv.URL, "http://")}) {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil || resp.StatusCode != 200 {
		b.r.t.Fatalf("stream: %v %v", resp, err)
	}
	return resp
}

// ended reports whether the stream's body ends within a few seconds.
func ended(resp *http.Response) bool {
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// A live stream ends with its session: a sign-out, a revoke from another device and
// an idle expiry all close it, and an open stream does not keep an idle session alive.
func TestALiveStreamDoesNotOutliveItsSession(t *testing.T) {
	r := newRig(t)
	r.be.events = make(chan domain.Event) // never delivers: the stream only ends with its session

	// sign-out
	b := r.browser().asDevice(ipad)
	b.signIn()
	resp := b.openStream()
	defer func() { _ = resp.Body.Close() }()
	csrf := csrfOf(t, b)
	b.do("POST", "/logout", url.Values{"csrf": {csrf}})
	if !ended(resp) {
		t.Error("the stream outlived the sign-out")
	}

	// revoke from another device
	phone, tablet := r.browser().asDevice(iphone), r.browser().asDevice(ipad)
	phone.signIn()
	r.advance(time.Minute)
	tablet.signIn()
	stream := phone.openStream()
	defer func() { _ = stream.Body.Close() }()
	_, page := tablet.do("GET", "/devices", nil)
	revoke := regexp.MustCompile(`action="/devices/([0-9a-f]+)/revoke"`).FindAllStringSubmatch(page, -1)
	tabletCSRF := csrfOf(t, tablet)
	tablet.do("POST", "/devices/"+revoke[1][1]+"/revoke", url.Values{"csrf": {tabletCSRF}})
	if !ended(stream) {
		t.Error("the stream outlived a revoke from another device")
	}

	// idle expiry: nothing but the open stream, which must not count as use
	idle := r.browser().asDevice(iphone)
	idle.signIn()
	s3 := idle.openStream()
	defer func() { _ = s3.Body.Close() }()
	r.advance(20 * time.Minute)
	if !ended(s3) {
		t.Error("an idle phone's stream kept its session alive")
	}
	if resp, _ := idle.do("GET", "/inbox", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("the session of an idle stream is still valid: %d", resp.StatusCode)
	}
}
