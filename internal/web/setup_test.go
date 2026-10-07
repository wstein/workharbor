package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/store"
)

type setupTestAuth struct{}

func (setupTestAuth) Session(r *http.Request) (Session, bool) {
	return Session{CSRF: "setup-csrf"}, r.Header.Get("X-Test-Auth") == "yes"
}
func (setupTestAuth) SignIn(http.ResponseWriter, *http.Request) error { return nil }
func (setupTestAuth) SignOut(http.ResponseWriter, *http.Request)      {}

type setupFunc func(context.Context) (doctor.Artifact, error)

func (f setupFunc) Check(ctx context.Context) (doctor.Artifact, error) { return f(ctx) }

func setupRig(t *testing.T, check Setup) (*Server, *time.Time) {
	t.Helper()
	st, err := store.Open(bg, filepath.Join(t.TempDir(), "setup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := t0
	s, err := New(&fake{}, Options{Auth: setupTestAuth{}, Store: st, Setup: check, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return s, &now
}

func setupRequest(s *Server, method, path string, form url.Values, auth bool, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, "http://setup.test"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if auth {
		r.Header.Set("X-Test-Auth", "yes")
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func setupForm(key string) url.Values { return url.Values{"csrf": {"setup-csrf"}, "key": {key}} }

func TestSetupReadsDoNotRunChecksAndRequireAuthentication(t *testing.T) {
	var calls atomic.Int32
	s, _ := setupRig(t, setupFunc(func(context.Context) (doctor.Artifact, error) { calls.Add(1); return doctor.Artifact{}, nil }))
	for _, path := range []string{"/setup", "/setup/report.json"} {
		if got := setupRequest(s, "GET", path, nil, false, ""); got.Code != 303 || got.Header().Get("Location") != "/login" {
			t.Fatalf("unauthenticated %s: %d", path, got.Code)
		}
	}
	if got := setupRequest(s, "POST", "/setup/check", setupForm("a"), false, "http://setup.test"); got.Code != 303 {
		t.Fatal(got.Code)
	}
	if got := setupRequest(s, "GET", "/setup", nil, true, ""); got.Code != 200 || !strings.Contains(got.Body.String(), "No checks have run") {
		t.Fatal(got.Body.String())
	}
	if got := setupRequest(s, "GET", "/setup/report.json", nil, true, ""); got.Code != 404 {
		t.Fatal(got.Code)
	}
	if calls.Load() != 0 {
		t.Fatal("GET ran a check")
	}
	s.opt.Setup = nil
	if got := setupRequest(s, "POST", "/setup/check", setupForm("b"), true, "http://setup.test"); got.Code != 404 {
		t.Fatal(got.Code)
	}
}

func TestSetupWriteGuardsRejectExecutionAndBadForms(t *testing.T) {
	var calls atomic.Int32
	s, _ := setupRig(t, setupFunc(func(context.Context) (doctor.Artifact, error) { calls.Add(1); return doctor.Artifact{}, nil }))
	for _, tc := range []struct {
		name, origin, path string
		form               url.Values
		status             int
	}{
		{"csrf", "http://setup.test", "/setup/check", url.Values{"csrf": {"bad"}, "key": {"a"}}, 403},
		{"missing key", "http://setup.test", "/setup/check", url.Values{"csrf": {"setup-csrf"}}, 400},
		{"other origin", "http://other.test", "/setup/check", setupForm("b"), 403},
		{"other scheme", "https://setup.test", "/setup/check", setupForm("b"), 403},
		{"missing origin", "", "/setup/check", setupForm("b"), 403},
		{"null origin", "null", "/setup/check", setupForm("b"), 403},
		{"host phase", "http://setup.test", "/setup/check", url.Values{"csrf": {"setup-csrf"}, "key": {"c"}, "phase": {"host"}}, 400},
		{"host step", "http://setup.test", "/setup/check", url.Values{"csrf": {"setup-csrf"}, "key": {"c"}, "only": {"power"}}, 400},
		{"query execution", "http://setup.test", "/setup/check?phase=host", setupForm("d"), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := setupRequest(s, "POST", tc.path, tc.form, true, tc.origin); got.Code != tc.status {
				t.Fatalf("got %d want %d", got.Code, tc.status)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("rejected form reached check")
	}
}

func TestSetupConfiguredHTTPSOriginThroughHTTPForwarder(t *testing.T) {
	var calls atomic.Int32
	s, _ := setupRig(t, setupFunc(func(context.Context) (doctor.Artifact, error) {
		calls.Add(1)
		return doctor.Artifact{}, nil
	}))
	s.opt.SetupOrigin = "https://setup.test"
	for _, origin := range []string{"http://setup.test", "https://other.test", "https://setup.test:444"} {
		if got := setupRequest(s, "POST", "/setup/check", setupForm(origin), true, origin); got.Code != 403 {
			t.Fatalf("origin %q: got %d, want 403", origin, got.Code)
		}
	}
	if got := setupRequest(s, "POST", "/setup/check", setupForm("forwarded"), true, "https://setup.test"); got.Code != 303 {
		t.Fatalf("HTTPS browser origin over HTTP forwarding: got %d, want 303", got.Code)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestSetupIdempotencyCooldownAndDownload(t *testing.T) {
	calls := 0
	report := doctor.PresentResults([]doctor.Result{{Check: "power", Status: doctor.Fail, Phase: doctor.PhaseHost, Detail: "wrong", Fix: "whr setup host --only power"}}).Artifact(t0, "v0.0.0", "web", "", "workharbor", false, "", "")
	s, now := setupRig(t, setupFunc(func(context.Context) (doctor.Artifact, error) { calls++; return report, nil }))
	for i := 0; i < 2; i++ {
		if got := setupRequest(s, "POST", "/setup/check", setupForm("same"), true, "http://setup.test"); got.Code != 303 {
			t.Fatal(got.Code)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	if got := setupRequest(s, "POST", "/setup/check", setupForm("fresh"), true, "http://setup.test"); got.Code != 429 {
		t.Fatal(got.Code)
	}
	*now = now.Add(setupCooldown)
	if got := setupRequest(s, "POST", "/setup/check", setupForm("fresh"), true, "http://setup.test"); got.Code != 303 {
		t.Fatal(got.Code)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	got := setupRequest(s, "GET", "/setup/report.json", nil, true, "")
	if got.Code != 200 || got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("Content-Disposition") != `attachment; filename="whr-setup-report.json"` {
		t.Fatal(got.Result().Header)
	}
	var downloaded doctor.Artifact
	if err := json.Unmarshal(got.Body.Bytes(), &downloaded); err != nil {
		t.Fatal(err)
	}
	if downloaded.Checks[0].Fix != report.Checks[0].Fix {
		t.Fatal(downloaded)
	}
	if calls != 2 {
		t.Fatal("download ran a check")
	}
	// Reusing the same key with a changed request must conflict, even after cooldown.
	form := setupForm("same")
	form.Set("csrf", "setup-csrf")
	form.Add("key", "extra")
	if got := setupRequest(s, "POST", "/setup/check", form, true, "http://setup.test"); got.Code != 409 {
		t.Fatal(got.Code)
	}
}

func TestSetupSingleFlightAndFailedCheckKeepsPreviousReport(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s, now := setupRig(t, setupFunc(func(context.Context) (doctor.Artifact, error) {
		calls.Add(1)
		close(started)
		<-release
		return doctor.Artifact{}, errors.New("private diagnostic error")
	}))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- setupRequest(s, "POST", "/setup/check", setupForm("one"), true, "http://setup.test") }()
	<-started
	if got := setupRequest(s, "POST", "/setup/check", setupForm("two"), true, "http://setup.test"); got.Code != 409 {
		t.Fatal(got.Code)
	}
	close(release)
	got := <-done
	if got.Code != 500 || strings.Contains(got.Body.String(), "private diagnostic error") {
		t.Fatal(got.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	if got := setupRequest(s, "POST", "/setup/check", setupForm("three"), true, "http://setup.test"); got.Code != 429 {
		t.Fatal(got.Code)
	}
	*now = now.Add(setupCooldown)
	s.opt.Setup = setupFunc(func(context.Context) (doctor.Artifact, error) { return doctor.Artifact{GeneratedAt: t0}, nil })
	if got := setupRequest(s, "POST", "/setup/check", setupForm("four"), true, "http://setup.test"); got.Code != 303 {
		t.Fatal(got.Code)
	}
	previous := s.setupSnapshot()
	*now = now.Add(setupCooldown)
	s.opt.Setup = setupFunc(func(context.Context) (doctor.Artifact, error) { return doctor.Artifact{}, errors.New("failed") })
	_ = setupRequest(s, "POST", "/setup/check", setupForm("five"), true, "http://setup.test")
	if s.setupSnapshot() != previous {
		t.Fatal("failed check replaced last report")
	}
}

func TestSetupBadgesFixesAndUnverifiedDetailsAreEscaped(t *testing.T) {
	results := []doctor.Result{}
	for _, status := range []doctor.Status{doctor.OK, doctor.Warn, doctor.Fail, doctor.NotVerified, doctor.Skipped} {
		results = append(results, doctor.Result{Check: string(status), Status: status, Phase: doctor.PhaseHost, Detail: `<script>bad()</script>`, Fix: `whr setup host --only power <img src=x onerror=bad()>`})
	}
	report := doctor.PresentResults(results).Artifact(t0, "v0.0.0", "web", "", "workharbor", false, "", "")
	s, _ := setupRig(t, setupFunc(func(context.Context) (doctor.Artifact, error) { return report, nil }))
	_ = setupRequest(s, "POST", "/setup/check", setupForm("one"), true, "http://setup.test")
	got := setupRequest(s, "GET", "/setup", nil, true, "")
	body := got.Body.String()
	for _, want := range []string{"setup-ok", "setup-warn", "setup-fail", "setup-not_verified", "setup-skipped", "setup-unverified", "Run in Terminal on the Mac as your administrator", "&lt;script&gt;", "&lt;img", "data-copy-command"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "<script>bad()") || strings.Contains(body, "<img src=x") || strings.Contains(body, "onclick=") {
		t.Fatal("unescaped active content")
	}
	if strings.Count(body, "data-copy-command") != 3 {
		t.Fatal("passing and skipped checks must not offer fixes")
	}
	if strings.Contains(got.Header().Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatal("inline CSP allowed")
	}
}
