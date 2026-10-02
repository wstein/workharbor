package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/forge/github"
	"github.com/wstein/workharbor/internal/redact"
)

const publicURL = "https://whr.example.test"

var bg = context.Background()

func testPEM(t *testing.T) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

// fakeGitHub answers the conversion and records what it was asked.
type fakeGitHub struct {
	ts     *httptest.Server
	pem    string
	mu     sync.Mutex
	calls  []string
	auth   []string
	status int
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{pem: testPEM(t), status: 201}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		status := f.status
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != 201 {
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 99, "slug": "workharbor-test", "html_url": "https://github.com/apps/workharbor-test",
			"pem": f.pem, "client_secret": "client-secret-value-0123456789", "webhook_secret": "webhook-secret-value-0123456789",
		})
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeGitHub) conversions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type rig struct {
	t      *testing.T
	gh     *fakeGitHub
	setup  *Setup
	srv    *httptest.Server
	keyDir string
	rd     *redact.Redactor
	now    time.Time
	mu     sync.Mutex
}

func newRig(t *testing.T, mut func(*Config)) *rig {
	t.Helper()
	r := &rig{t: t, gh: newFakeGitHub(t), rd: redact.New(), now: time.Unix(1_800_000_000, 0), keyDir: filepath.Join(t.TempDir(), "whr")}
	cfg := Config{
		PublicURL: publicURL, Name: "workharbor-test", KeyDir: r.keyDir,
		GitHubURL: "https://github.com", APIURL: r.gh.ts.URL, Redactor: r.rd, TTL: 5 * time.Minute,
		Now: func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
	}
	if mut != nil {
		mut(&cfg)
	}
	s, err := NewSetup(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r.setup = s
	r.srv = httptest.NewServer(s.Handler())
	t.Cleanup(r.srv.Close)
	return r
}

func (r *rig) advance(d time.Duration) { r.mu.Lock(); r.now = r.now.Add(d); r.mu.Unlock() }

func (r *rig) get(path string, q url.Values) (int, string) {
	r.t.Helper()
	resp, err := http.Get(r.srv.URL + path + "?" + q.Encode()) //nolint:noctx,gosec // a test against a local server
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func stateOf(t *testing.T, startURL string) string {
	t.Helper()
	u, err := url.Parse(startURL)
	if err != nil || u.Query().Get("state") == "" {
		t.Fatalf("start URL %q has no state", startURL)
	}
	return u.Query().Get("state")
}

func (r *rig) noKey() {
	r.t.Helper()
	if _, err := os.Stat(r.keyDir); err == nil {
		ents, _ := os.ReadDir(r.keyDir)
		if len(ents) != 0 {
			r.t.Errorf("a refused request left files behind: %v", ents)
		}
	}
}

func TestTheWholeFlowCreatesTheKeyAndReportsNoSecret(t *testing.T) {
	r := newRig(t, nil)
	start, _ := r.setup.StartURL()
	state := stateOf(t, start)
	if !strings.HasPrefix(start, publicURL+StartPath+"?state=") {
		t.Fatalf("start URL %q", start)
	}

	code, page := r.get(StartPath, url.Values{"state": {state}})
	if code != 200 || !strings.Contains(page, `action="https://github.com/settings/apps/new?state=`+state+`"`) {
		t.Fatalf("start page: %d\n%s", code, page)
	}

	code, body := r.get(CallbackPath, url.Values{"code": {"c0de1234abcd"}, "state": {state}})
	if code != 200 || !strings.Contains(body, "https://github.com/apps/workharbor-test/installations/new") {
		t.Fatalf("callback: %d\n%s", code, body)
	}
	for _, secret := range []string{r.gh.pem, "client-secret-value", "webhook-secret-value", "BEGIN RSA"} {
		if strings.Contains(body, secret) || strings.Contains(page, secret) {
			t.Errorf("a page shows %q", secret)
		}
	}

	res, err := r.setup.Wait(bg)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(r.keyDir, "github-app-99.pem")
	if res.AppID != 99 || res.Slug != "workharbor-test" || res.KeyFile != want || res.InstallURL != "https://github.com/apps/workharbor-test/installations/new" {
		t.Fatalf("result %+v", res)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), "secret") {
		t.Errorf("result %s", b)
	}
	if got, err := os.ReadFile(filepath.Clean(want)); err != nil || string(got) != r.gh.pem {
		t.Fatalf("key file content: %v", err)
	}
	if fi, _ := os.Stat(want); fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v, want 0600", fi.Mode().Perm())
	}
	if _, err := github.ParsePrivateKey([]byte(r.gh.pem)); err != nil {
		t.Fatalf("the fake's key is not usable: %v", err)
	}
	// the conversion needs no credential; the redactor learned the key and both secrets
	if r.gh.calls[0] != "POST /app-manifests/c0de1234abcd/conversions" || r.gh.auth[0] != "" {
		t.Errorf("conversion call %v auth %q", r.gh.calls, r.gh.auth)
	}
	for _, secret := range []string{r.gh.pem, "client-secret-value-0123456789", "webhook-secret-value-0123456789"} {
		if got := r.rd.String("leak: " + secret); strings.Contains(got, secret) {
			t.Errorf("the redactor does not know %.20q", secret)
		}
	}
}

func TestTheManifestAsksForExactlyWhatWorkharborNeeds(t *testing.T) {
	r := newRig(t, nil)
	m := r.setup.Manifest()
	if m["public"] != false || m["redirect_url"] != publicURL+CallbackPath || m["name"] != "workharbor-test" || m["url"] != publicURL {
		t.Errorf("manifest %v", m)
	}
	if h := m["hook_attributes"].(map[string]any); h["active"] != false {
		t.Errorf("the hook must be inactive: %v", h)
	}
	if ev := m["default_events"].([]string); len(ev) != 0 {
		t.Errorf("events %v", ev)
	}
	if !reflect.DeepEqual(m["default_permissions"], map[string]string{"contents": "write", "issues": "write", "pull_requests": "write", "metadata": "read"}) {
		t.Errorf("permissions %v", m["default_permissions"])
	}
}

// A configured board adds its permission to the manifest, and only then.
func TestTheManifestAddsTheBoardPermissionOnlyWhenAsked(t *testing.T) {
	plain := newRig(t, nil)
	if p := plain.setup.Manifest()["default_permissions"].(map[string]string); p[github.BoardPermission] != "" || len(p) != 4 {
		t.Errorf("without a board: %v", p)
	}
	withBoard := newRig(t, func(c *Config) { c.Board = true })
	if p := withBoard.setup.Manifest()["default_permissions"].(map[string]string); p[github.BoardPermission] != "write" || len(p) != 5 {
		t.Errorf("with a board: %v", p)
	}
}

func TestAForgedStateIsRefusedAndLeavesNothing(t *testing.T) {
	r := newRig(t, nil)
	start, _ := r.setup.StartURL()
	good := stateOf(t, start)
	for _, st := range []string{"", "forged", strings.Repeat("a", len(good)), good[:len(good)-1], good + "0"} {
		if code, _ := r.get(CallbackPath, url.Values{"code": {"c0de1234abcd"}, "state": {st}}); code != 400 {
			t.Errorf("state %q: status %d, want 400", st, code)
		}
		if code, _ := r.get(StartPath, url.Values{"state": {st}}); code != 400 {
			t.Errorf("start with state %q: status %d, want 400", st, code)
		}
	}
	if r.gh.conversions() != 0 {
		t.Fatal("a forged state reached GitHub")
	}
	r.noKey()
	// the real state still works: refusals do not spend it
	if code, _ := r.get(CallbackPath, url.Values{"code": {"c0de1234abcd"}, "state": {good}}); code != 200 {
		t.Errorf("the genuine state: %d", code)
	}
}

func TestAReplayedStateIsRefused(t *testing.T) {
	r := newRig(t, nil)
	start, _ := r.setup.StartURL()
	state := stateOf(t, start)
	q := url.Values{"code": {"c0de1234abcd"}, "state": {state}}
	if code, _ := r.get(CallbackPath, q); code != 200 {
		t.Fatalf("first: %d", code)
	}
	if code, _ := r.get(CallbackPath, q); code != 400 {
		t.Errorf("replay: %d, want 400", code)
	}
	if r.gh.conversions() != 1 {
		t.Errorf("%d conversions, want 1", r.gh.conversions())
	}
}

func TestAnExpiredStateIsRefused(t *testing.T) {
	r := newRig(t, nil)
	start, exp := r.setup.StartURL()
	state := stateOf(t, start)
	if !exp.Equal(r.now.Add(5 * time.Minute)) {
		t.Errorf("expiry %v", exp)
	}
	r.advance(5*time.Minute + time.Second)
	if code, _ := r.get(CallbackPath, url.Values{"code": {"c0de1234abcd"}, "state": {state}}); code != 400 {
		t.Errorf("expired callback: %d", code)
	}
	if code, _ := r.get(StartPath, url.Values{"state": {state}}); code != 400 {
		t.Errorf("expired start: %d", code)
	}
	if r.gh.conversions() != 0 {
		t.Error("an expired state reached GitHub")
	}
	r.noKey()
}

func TestAFailingConversionWritesNothingAndSpendsTheState(t *testing.T) {
	r := newRig(t, nil)
	r.gh.status = 404
	start, _ := r.setup.StartURL()
	state := stateOf(t, start)
	code, body := r.get(CallbackPath, url.Values{"code": {"c0de1234abcd"}, "state": {state}})
	if code != 502 || strings.Contains(body, "c0de1234abcd") {
		t.Fatalf("status %d, body %q", code, body)
	}
	r.noKey()
	if code, _ := r.get(CallbackPath, url.Values{"code": {"c0de1234abcd"}, "state": {state}}); code != 400 {
		t.Errorf("retry with the spent state: %d", code)
	}
	ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	defer cancel()
	if _, err := r.setup.Wait(ctx); err == nil {
		t.Error("a failed conversion produced a result")
	}
}

func TestACodeThatIsNotACodeNeverReachesGitHub(t *testing.T) {
	r := newRig(t, nil)
	for _, c := range []string{"", "x", "../../app/installations", "a b c d e f g h", "abc/def/ghijk"} {
		start, _ := r.setup.StartURL()
		if code, _ := r.get(CallbackPath, url.Values{"code": {c}, "state": {stateOf(t, start)}}); code != 400 {
			t.Errorf("code %q: %d", c, code)
		}
	}
	if r.gh.conversions() != 0 {
		t.Error("an invalid code reached GitHub")
	}
}

func TestAnOrganizationOwnerTargetsTheOrganization(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Org = "acme-inc" })
	start, _ := r.setup.StartURL()
	state := stateOf(t, start)
	_, page := r.get(StartPath, url.Values{"state": {state}})
	if !strings.Contains(page, `action="https://github.com/organizations/acme-inc/settings/apps/new?state=`) {
		t.Errorf("page:\n%s", page)
	}
}

func TestNothingElseIsServed(t *testing.T) {
	r := newRig(t, nil)
	for _, p := range []string{"/", "/v1/tasks", "/github/app", "/github/app/hook", "/github/app/callback/x"} {
		if code, _ := r.get(p, nil); code != 404 {
			t.Errorf("GET %s: %d, want 404", p, code)
		}
	}
	resp, err := http.Post(r.srv.URL+CallbackPath+"?state=x&code=y", "text/plain", strings.NewReader("")) //nolint:noctx // a test against a local server
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Errorf("POST: %d, want a 4xx", resp.StatusCode)
	}
}

func TestTheKeyFileIsExclusiveAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	path, err := WriteKey(dir, 7, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(dir)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("file %v, dir %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	if _, err := WriteKey(dir, 7, []byte("second")); err == nil || !strings.Contains(err.Error(), "not overwritten") {
		t.Errorf("an existing key was not refused: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Clean(path)); string(b) != "first" {
		t.Errorf("the key was overwritten: %q", b)
	}
	// a planted symbolic link is not followed
	target := filepath.Join(t.TempDir(), "victim")
	if err := os.Symlink(target, filepath.Join(dir, KeyFileName(8))); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteKey(dir, 8, []byte("x")); err == nil {
		t.Error("a symbolic link was followed")
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("the link's target was created")
	}
	if _, err := WriteKey("relative/dir", 9, nil); err == nil {
		t.Error("a relative directory was accepted")
	}
}

func TestSetupRefusesWhatIsNotSafe(t *testing.T) {
	bad := map[string]func(*Config){
		"http public":    func(c *Config) { c.PublicURL = "http://whr.example.test" },
		"public path":    func(c *Config) { c.PublicURL = publicURL + "/x" },
		"bad name":       func(c *Config) { c.Name = "no/slash" },
		"long name":      func(c *Config) { c.Name = strings.Repeat("a", 35) },
		"bad org":        func(c *Config) { c.Org = "../evil" },
		"http github":    func(c *Config) { c.GitHubURL = "http://github.example.test" },
		"no key dir":     func(c *Config) { c.KeyDir = "" },
		"api with creds": func(c *Config) { c.APIURL = "https://user:pw@api.example.test" },
	}
	for name, mut := range bad {
		cfg := Config{PublicURL: publicURL, Name: "ok-name", KeyDir: "/x"}
		mut(&cfg)
		if _, err := NewSetup(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewSetup(Config{PublicURL: "http://127.0.0.1:8787", Name: "ok", KeyDir: "/x"}); err != nil {
		t.Errorf("a loopback test double was refused: %v", err)
	}
}

func TestStatesAreSingleUseAndCompareEveryLiveOne(t *testing.T) {
	now := time.Unix(0, 0)
	s := NewStates(time.Minute, func() time.Time { return now })
	a, _ := s.Mint("")
	b, _ := s.Mint("acme")
	if a == b || len(a) != 64 {
		t.Fatalf("states %q %q", a, b)
	}
	if org, ok := s.Peek(b); !ok || org != "acme" {
		t.Errorf("peek: %q %v", org, ok)
	}
	if _, ok := s.Take(b); !ok {
		t.Fatal("take")
	}
	if _, ok := s.Take(b); ok {
		t.Error("a state was used twice")
	}
	now = now.Add(time.Minute)
	if _, ok := s.Take(a); ok {
		t.Error("an expired state was taken")
	}
}
