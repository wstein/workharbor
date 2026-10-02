package github

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/policy"
	"github.com/wstein/workharbor/internal/redact"
)

var bg = context.Background()

var (
	keyOnce sync.Once
	testKey *rsa.PrivateKey
)

func key(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testKey = k
	})
	return testKey
}

// fakeGitHub is a GitHub API whose answers a test sets. It checks the JWT of an
// App request and the installation token of every other request.
type fakeGitHub struct {
	t   *testing.T
	ts  *httptest.Server
	key *rsa.PublicKey
	now func() time.Time

	mu        sync.Mutex
	mints     int
	tokenSeq  int
	reqs      []string
	bodies    map[string]string
	expireIn  time.Duration
	revoke    bool // refuse the current token once with 401
	issues    map[int]string
	branch    map[string]string
	handlers  map[string]func(w http.ResponseWriter, r *http.Request)
	lastRepos []string
	validTok  map[string]bool
	// denyBoardMint makes the installation refuse a token that asks for the board's permission.
	denyBoardMint bool
}

func newFake(t *testing.T, now func() time.Time) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, key: &key(t).PublicKey, now: now, bodies: map[string]string{}, expireIn: time.Hour, handlers: map[string]func(http.ResponseWriter, *http.Request){}, validTok: map[string]bool{}, branch: map[string]string{}}
	f.ts = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeGitHub) jwtOK(raw string) bool {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(f.key, crypto.SHA256, sum[:], sig) != nil {
		return false
	}
	var hdr map[string]string
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if json.Unmarshal(hb, &hdr) != nil || hdr["alg"] != "RS256" {
		return false
	}
	var c struct {
		Iat, Exp, Iss int64
	}
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if json.Unmarshal(cb, &c) != nil {
		return false
	}
	now := f.now().Unix()
	return c.Iss == 4242 && c.Iat < now && c.Exp > now && c.Exp-c.Iat <= 600
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	route := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.reqs = append(f.reqs, route)
	f.bodies[route] = string(b)
	h := f.handlers[route]
	f.mu.Unlock()
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") == "" {
		reply(400, map[string]string{"message": "bad headers"})
		return
	}
	if r.URL.Path == "/app" || strings.HasPrefix(r.URL.Path, "/app/") || strings.HasSuffix(r.URL.Path, "/installation") {
		if !f.jwtOK(bearer) {
			reply(401, map[string]string{"message": "A JSON web token could not be decoded"})
			return
		}
	} else {
		f.mu.Lock()
		ok := f.validTok[bearer]
		revoke := f.revoke
		if revoke {
			f.revoke = false
			delete(f.validTok, bearer)
		}
		f.mu.Unlock()
		if !ok || revoke {
			reply(401, map[string]string{"message": "Bad credentials"})
			return
		}
	}
	if h != nil {
		h(w, r)
		return
	}
	if f.denyBoardMint && strings.HasSuffix(r.URL.Path, "/access_tokens") && strings.Contains(string(b), BoardPermission) {
		reply(422, map[string]string{"message": "The permissions requested are not granted to this installation."})
		return
	}
	switch {
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/installation"):
		reply(200, map[string]int{"id": 7})
	case r.Method == "POST" && r.URL.Path == "/app/installations/7/access_tokens":
		var req struct {
			Repositories []string          `json:"repositories"`
			Permissions  map[string]string `json:"permissions"`
		}
		_ = json.Unmarshal(b, &req)
		f.mu.Lock()
		f.mints++
		f.tokenSeq++
		tok := "ghs_installationtoken" + strings.Repeat("x", 20) + strconv.Itoa(f.tokenSeq)
		f.validTok[tok] = true
		f.lastRepos = req.Repositories
		exp := f.now().Add(f.expireIn)
		f.mu.Unlock()
		reply(201, map[string]any{"token": tok, "expires_at": exp.UTC().Format(time.RFC3339), "permissions": req.Permissions})
	default:
		reply(404, map[string]string{"message": "Not Found"})
	}
}

func (f *fakeGitHub) client(t *testing.T, mut func(*Config)) *Client {
	t.Helper()
	cfg := Config{AppID: 4242, Key: key(t), Repos: []string{"wstein/workharbor"}, BaseURL: f.ts.URL, Now: f.now}
	if mut != nil {
		mut(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fakeGitHub) count(route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if r == route {
			n++
		}
	}
	return n
}

func clock() (func() time.Time, func(time.Duration)) {
	var mu sync.Mutex
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
		func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
}

func TestAnInstallationTokenIsMintedFromASignedJWTAndScopedToTheRepository(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	rd := redact.New()
	c := f.client(t, func(c *Config) { c.Redactor = rd })
	f.issues = map[int]string{}
	f.handlers["GET /repos/wstein/workharbor/issues/7"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"number":7,"title":"T","body":"B","author_association":"OWNER","user":{"login":"wstein"}}`)
	}
	if _, err := c.GetIssue(bg, "wstein/workharbor", 7); err != nil {
		t.Fatal(err)
	}
	if f.mints != 1 || len(f.lastRepos) != 1 || f.lastRepos[0] != "workharbor" {
		t.Errorf("mints %d, scoped to %v: the token must be for the one repository", f.mints, f.lastRepos)
	}
	var perms map[string]any
	var req struct {
		Permissions map[string]any `json:"permissions"`
	}
	_ = json.Unmarshal([]byte(f.bodies["POST /app/installations/7/access_tokens"]), &req)
	perms = req.Permissions
	if perms["issues"] != "write" || perms["pull_requests"] != "write" || perms["contents"] != "write" || perms["metadata"] != "read" || len(perms) != 4 {
		t.Errorf("permissions = %v, want exactly issues, pull_requests, contents write and metadata read", perms)
	}
	tok, _ := c.InstallationToken(bg, "wstein/workharbor")
	if got := rd.String("leaked " + tok + " here"); strings.Contains(got, tok) {
		t.Error("the installation token was not registered with the redactor")
	}
}

func TestTheTokenIsCachedUntilItNearsItsExpiryThenMintedAgain(t *testing.T) {
	now, advance := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	f.handlers["POST /repos/wstein/workharbor/issues/1/comments"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); _, _ = io.WriteString(w, "{}") }
	for range 3 {
		if err := c.CommentIssue(bg, "wstein/workharbor", 1, "hi"); err != nil {
			t.Fatal(err)
		}
	}
	if f.mints != 1 || f.count("GET /repos/wstein/workharbor/installation") != 1 {
		t.Errorf("minted %d times, looked up the installation %d times, want once each", f.mints, f.count("GET /repos/wstein/workharbor/installation"))
	}
	advance(59 * time.Minute) // a minute before expiry: renew
	if err := c.CommentIssue(bg, "wstein/workharbor", 1, "hi"); err != nil {
		t.Fatal(err)
	}
	if f.mints != 2 {
		t.Errorf("minted %d times after the token neared its expiry, want 2", f.mints)
	}
	if f.count("GET /repos/wstein/workharbor/installation") != 1 {
		t.Error("the installation ID must be remembered")
	}
}

func TestARevokedTokenIsRetriedOnceWithAFreshOne(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	f.handlers["POST /repos/wstein/workharbor/issues/1/comments"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); _, _ = io.WriteString(w, "{}") }
	if err := c.CommentIssue(bg, "wstein/workharbor", 1, "one"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.revoke = true
	f.mu.Unlock()
	if err := c.CommentIssue(bg, "wstein/workharbor", 1, "two"); err != nil {
		t.Fatalf("a revoked token must be replaced: %v", err)
	}
	if f.mints != 2 {
		t.Errorf("mints = %d, want 2", f.mints)
	}
}

func TestOnlyConfiguredRepositoriesAreReached(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	for name, call := range map[string]func() error{
		"issue":   func() error { _, err := c.GetIssue(bg, "evil/other", 1); return err },
		"comment": func() error { return c.CommentIssue(bg, "evil/other", 1, "x") },
		"branch":  func() error { _, err := c.BranchSHA(bg, "evil/other", "main"); return err },
		"pr":      func() error { _, err := c.OpenPR(bg, "evil/other", "agent/x", "abc", "t", "b"); return err },
		"token":   func() error { _, err := c.InstallationToken(bg, "evil/other"); return err },
	} {
		if err := call(); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s = %v, want ErrNotAllowed", name, err)
		}
	}
	if len(f.reqs) != 0 {
		t.Errorf("a refused repository sent requests: %v", f.reqs)
	}
	if c.GetIssueCaseOK(t) {
		t.Log("case-insensitive repository names are accepted")
	}
}

// GetIssueCaseOK reports whether the repository check ignores case, as GitHub does.
func (c *Client) GetIssueCaseOK(t *testing.T) bool {
	t.Helper()
	return c.allowed("WSTEIN/Workharbor") == nil
}

func TestGetIssueReturnsTheAuthorAssociationAndRefusesPullRequests(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	f.handlers["GET /repos/wstein/workharbor/issues/5"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"number":5,"title":"Fix","body":"text","author_association":"CONTRIBUTOR","user":{"login":"someone"}}`)
	}
	f.handlers["GET /repos/wstein/workharbor/issues/6"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"number":6,"title":"PR","pull_request":{"url":"x"},"user":{"login":"a"}}`)
	}
	got, err := c.GetIssue(bg, "wstein/workharbor", 5)
	if err != nil || got != (forge.Issue{Repo: "wstein/workharbor", Number: 5, Title: "Fix", Body: "text", Author: "someone", AuthorAssociation: "CONTRIBUTOR"}) {
		t.Errorf("issue = %+v, %v", got, err)
	}
	if _, err := c.GetIssue(bg, "wstein/workharbor", 6); !errors.Is(err, ErrIsPR) {
		t.Errorf("a pull request = %v, want ErrIsPR", err)
	}
	if _, err := c.GetIssue(bg, "wstein/workharbor", 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown issue = %v, want ErrNotFound", err)
	}
	if _, err := c.GetIssue(bg, "wstein/workharbor", 0); err == nil {
		t.Error("issue number 0 was accepted")
	}
}

func TestBranchSHAHandlesSlashesAndMissingBranches(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	f.handlers["GET /repos/wstein/workharbor/git/ref/heads/agent/docs"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ref":"refs/heads/agent/docs","object":{"sha":"abc123"}}`)
	}
	if sha, err := c.BranchSHA(bg, "wstein/workharbor", "agent/docs"); err != nil || sha != "abc123" {
		t.Errorf("sha = %q, %v", sha, err)
	}
	if _, err := c.BranchSHA(bg, "wstein/workharbor", "agent/nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing branch = %v, want ErrNotFound", err)
	}
	for _, bad := range []string{"", "/x", "x/", "a//b", "a/../b"} {
		if _, err := c.BranchSHA(bg, "wstein/workharbor", bad); err == nil {
			t.Errorf("branch %q was accepted", bad)
		}
	}
}

func TestOpenPRChecksTheBranchAndOpensAgainstTheDefaultBranch(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	f.handlers["GET /repos/wstein/workharbor/git/ref/heads/agent/docs"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"object":{"sha":"approved1"}}`)
	}
	f.handlers["GET /repos/wstein/workharbor"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"default_branch":"develop"}`)
	}
	f.handlers["POST /repos/wstein/workharbor/pulls"] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"number":12,"html_url":"https://github.com/wstein/workharbor/pull/12","head":{"sha":"approved1"}}`)
	}
	f.handlers["PATCH /repos/wstein/workharbor/pulls/12"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{}") }

	// A branch that moved after the approval is not published, and nothing is posted.
	if _, err := c.OpenPR(bg, "wstein/workharbor", "agent/docs", "otherSHA", "t", "b"); !errors.Is(err, ErrStale) {
		t.Fatalf("a moved branch = %v, want ErrStale", err)
	}
	if f.count("POST /repos/wstein/workharbor/pulls") != 0 {
		t.Fatal("a pull request was opened for a branch at another commit")
	}
	pr, err := c.OpenPR(bg, "wstein/workharbor", "agent/docs", "approved1", "Add docs", "body")
	if err != nil || pr.Number != 12 || pr.URL != "https://github.com/wstein/workharbor/pull/12" || pr.SHA != "approved1" || pr.Branch != "agent/docs" {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	var sent map[string]string
	_ = json.Unmarshal([]byte(f.bodies["POST /repos/wstein/workharbor/pulls"]), &sent)
	if sent["head"] != "agent/docs" || sent["base"] != "develop" || sent["title"] != "Add docs" {
		t.Errorf("body = %v", sent)
	}
	if err := c.UpdatePR(bg, pr, "approved1", "New title", "new body"); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdatePR(bg, pr, "other", "t", "b"); !errors.Is(err, ErrStale) {
		t.Errorf("update at another commit = %v, want ErrStale", err)
	}
	if err := c.UpdatePR(bg, forge.PullRequest{Repo: "wstein/workharbor", Branch: "agent/docs"}, "approved1", "t", "b"); err == nil {
		t.Error("a pull request without a number was accepted")
	}
}

func TestRateLimitsAndAuthFailuresAreTyped(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	reset := now().Add(30 * time.Minute)
	f.handlers["GET /repos/wstein/workharbor/issues/1"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", itoa(reset.Unix()))
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"message":"API rate limit exceeded"}`)
	}
	f.handlers["GET /repos/wstein/workharbor/issues/2"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"message":"secondary rate limit"}`)
	}
	f.handlers["GET /repos/wstein/workharbor/issues/3"] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"message":"Resource not accessible by integration"}`)
	}
	_, err := c.GetIssue(bg, "wstein/workharbor", 1)
	var ae *APIError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &ae) || ae.Status != 403 || !ae.RetryAfter.Equal(reset) {
		t.Errorf("primary limit = %v (%+v)", err, ae)
	}
	_, err = c.GetIssue(bg, "wstein/workharbor", 2)
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &ae) || !ae.RetryAfter.Equal(now().Add(42*time.Second)) {
		t.Errorf("secondary limit = %v", err)
	}
	if _, err = c.GetIssue(bg, "wstein/workharbor", 3); !errors.Is(err, ErrAuth) || errors.Is(err, ErrRateLimited) {
		t.Errorf("a refusal = %v, want ErrAuth and not a rate limit", err)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestNoSecretReachesAnErrorOrALog(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	rd := redact.New()
	c := f.client(t, func(c *Config) { c.Redactor = rd })
	tok, err := c.InstallationToken(bg, "wstein/workharbor")
	if err != nil {
		t.Fatal(err)
	}
	// GitHub echoes the credential in an error message: it must not come back out.
	f.handlers["GET /repos/wstein/workharbor/issues/1"] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"message":"boom `+tok+` boom"}`)
	}
	_, err = c.GetIssue(bg, "wstein/workharbor", 1)
	if err == nil || strings.Contains(err.Error(), tok) {
		t.Errorf("the token is in the error: %v", err)
	}
	// A transport error carries neither the URL nor the header.
	c2 := f.client(t, func(c *Config) { c.BaseURL = "http://127.0.0.1:1" })
	_, err = c2.GetIssue(bg, "wstein/workharbor", 1)
	if err == nil || strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "eyJ") {
		t.Errorf("a transport error leaked a credential: %v", err)
	}
}

func TestTheBaseURLMustBeHTTPSOrLoopback(t *testing.T) {
	for _, bad := range []string{"http://api.example.com", "ftp://x", "https://user:pw@x.example", "://", "https://"} {
		if _, err := New(Config{AppID: 1, Key: key(t), Repos: []string{"a/b"}, BaseURL: bad}); err == nil {
			t.Errorf("base URL %q was accepted", bad)
		}
	}
	for _, good := range []string{"https://api.github.com", "https://ghe.example.com/api/v3", "http://127.0.0.1:8080", "http://[::1]:9"} {
		if _, err := New(Config{AppID: 1, Key: key(t), Repos: []string{"a/b"}, BaseURL: good}); err != nil {
			t.Errorf("base URL %q = %v", good, err)
		}
	}
	for name, cfg := range map[string]Config{
		"no app":   {Key: key(t), Repos: []string{"a/b"}},
		"no key":   {AppID: 1, Repos: []string{"a/b"}},
		"no repo":  {AppID: 1, Key: key(t)},
		"bad repo": {AppID: 1, Key: key(t), Repos: []string{"nope"}},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestParsePrivateKey(t *testing.T) {
	k := key(t)
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	b8, _ := x509.MarshalPKCS8PrivateKey(k)
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b8})
	for name, in := range map[string][]byte{"pkcs1": pkcs1, "pkcs8": pkcs8} {
		got, err := ParsePrivateKey(in)
		if err != nil || !got.Equal(k) {
			t.Errorf("%s = %v", name, err)
		}
	}
	for name, in := range map[string][]byte{"empty": nil, "text": []byte("not pem"), "garbage": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("xx")})} {
		_, err := ParsePrivateKey(in)
		if err == nil {
			t.Errorf("%s was accepted", name)
		} else if strings.Contains(err.Error(), "xx") {
			t.Errorf("%s: the error repeats the key: %v", name, err)
		}
	}
}

func TestVerifyWebhook(t *testing.T) {
	secret := []byte("hook-secret")
	c, err := New(Config{AppID: 1, Key: key(t), Repos: []string{"a/b"}, WebhookSecret: secret})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	h := http.Header{}
	h.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	if err := c.VerifyWebhook(h, body); err != nil {
		t.Errorf("a valid signature: %v", err)
	}
	if err := c.VerifyWebhook(h, []byte(`{"action":"closed"}`)); err == nil {
		t.Error("a changed body was accepted")
	}
	for _, bad := range []string{"", "sha256=", "sha256=zz", "sha1=abc", "abc"} {
		h2 := http.Header{}
		h2.Set("X-Hub-Signature-256", bad)
		if err := c.VerifyWebhook(h2, body); err == nil {
			t.Errorf("signature %q was accepted", bad)
		}
	}
	none, _ := New(Config{AppID: 1, Key: key(t), Repos: []string{"a/b"}})
	if err := none.VerifyWebhook(h, body); err == nil {
		t.Error("webhooks were accepted without a secret")
	}
}

// The adapter works behind the Guard: a push or a PR needs the approved commit.
func TestBehindTheGuardAPRNeedsAnApprovedCommit(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	f.handlers["GET /repos/wstein/workharbor/git/ref/heads/agent/docs"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"object":{"sha":"sha-ok"}}`)
	}
	f.handlers["GET /repos/wstein/workharbor"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"default_branch":"main"}`) }
	f.handlers["POST /repos/wstein/workharbor/pulls"] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"number":3,"html_url":"https://github.com/wstein/workharbor/pull/3"}`)
	}
	g := forge.NewGuard(c, nil, policy.Default(), approvedOnly{"d1", "sha-ok"})
	if _, err := g.OpenPR(bg, "wstein/workharbor", "agent/docs", forge.Approval{DecisionID: "d1", SHA: "other"}, "t", "b"); !errors.Is(err, forge.ErrNotApproved) {
		t.Errorf("an unapproved commit = %v", err)
	}
	if _, err := g.OpenPR(bg, "wstein/workharbor", "main", forge.Approval{DecisionID: "d1", SHA: "sha-ok"}, "t", "b"); !errors.Is(err, forge.ErrBranch) {
		t.Errorf("a non-agent branch = %v", err)
	}
	if f.count("POST /repos/wstein/workharbor/pulls") != 0 {
		t.Fatal("a refused request reached GitHub")
	}
	if pr, err := g.OpenPR(bg, "wstein/workharbor", "agent/docs", forge.Approval{DecisionID: "d1", SHA: "sha-ok"}, "t", "b"); err != nil || pr.Number != 3 {
		t.Errorf("an approved commit = %+v, %v", pr, err)
	}
}

type approvedOnly struct{ id, sha string }

func (a approvedOnly) Approved(_ context.Context, ap forge.Approval) bool {
	return ap.DecisionID == a.id && ap.SHA == a.sha
}

func TestTheAppJWTIsShortLivedAndBackdated(t *testing.T) {
	now, _ := clock()
	c, err := New(Config{AppID: 4242, Key: key(t), Repos: []string{"a/b"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.appJWT()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGitHub{key: &key(t).PublicKey, now: now}
	if !f.jwtOK(raw) {
		t.Error("the JWT does not verify with the App's public key, or its claims are wrong")
	}
	parts := strings.Split(raw, ".")
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct{ Iat, Exp int64 }
	_ = json.Unmarshal(cb, &claims)
	if claims.Iat != now().Add(-time.Minute).Unix() || claims.Exp-claims.Iat != 600 {
		t.Errorf("claims %+v: want iat a minute ago and a ten minute span", claims)
	}
}

func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestCheckAppFindsAMissingInstallationAndWrongPermissions(t *testing.T) {
	now := func() time.Time { return time.Unix(1_800_000_000, 0) }
	f := newFake(t, now)
	f.handlers["GET /app"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"id": 4242, "slug": "workharbor-x", "permissions": AppPermissions()})
	}
	f.handlers["GET /repos/wstein/workharbor/installation"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"id": 7, "permissions": AppPermissions()})
	}
	f.handlers["GET /repos/wstein/other/installation"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 404, map[string]string{"message": "Not Found"})
	}
	f.handlers["GET /repos/wstein/wide/installation"] = func(w http.ResponseWriter, _ *http.Request) {
		p := AppPermissions()
		p["contents"] = "read"
		p["administration"] = "write"
		delete(p, "issues")
		jsonReply(w, 200, map[string]any{"id": 8, "permissions": p})
	}
	c := f.client(t, func(cfg *Config) { cfg.Repos = []string{"wstein/workharbor", "wstein/other", "wstein/wide"} })
	rep, err := c.CheckApp(bg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || rep.Slug != "workharbor-x" || len(rep.Problems) != 0 {
		t.Fatalf("%+v", rep)
	}
	byRepo := map[string]RepoReport{}
	for _, r := range rep.Repos {
		byRepo[r.Repo] = r
	}
	if r := byRepo["wstein/workharbor"]; !r.Installed || len(r.Problems) != 0 {
		t.Errorf("exact installation: %+v", r)
	}
	if r := byRepo["wstein/other"]; r.Installed {
		t.Errorf("a 404 is not an installation: %+v", r)
	}
	r := byRepo["wstein/wide"]
	joined := strings.Join(r.Problems, "|")
	for _, want := range []string{"read on contents, want write", "nothing on issues, want write", "write on administration, which is not wanted"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems %q lack %q", joined, want)
		}
	}
}

func TestCheckAppSaysSoWhenAllIsRight(t *testing.T) {
	now := func() time.Time { return time.Unix(1_800_000_000, 0) }
	f := newFake(t, now)
	f.handlers["GET /app"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"id": 4242, "slug": "s", "permissions": AppPermissions()})
	}
	f.handlers["GET /repos/wstein/workharbor/installation"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"id": 7, "permissions": AppPermissions()})
	}
	rep, err := f.client(t, nil).CheckApp(bg)
	if err != nil || !rep.OK() {
		t.Fatalf("%+v, %v", rep, err)
	}
}

func TestCheckAppReportsABadKeyAsAnAuthError(t *testing.T) {
	now := func() time.Time { return time.Unix(1_800_000_000, 0) }
	f := newFake(t, now)
	f.handlers["GET /app"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 401, map[string]string{"message": "A JSON web token could not be decoded"})
	}
	if _, err := f.client(t, nil).CheckApp(bg); !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}

// RevokeTokens is the token half of kill-all: each held token revokes itself
// with DELETE /installation/token, and the next call mints a new one.
func TestRevokeTokensRevokesWhatIsHeldAndForgetsIt(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	first, err := c.InstallationToken(bg, "wstein/workharbor")
	if err != nil {
		t.Fatal(err)
	}
	f.handlers["DELETE /installation/token"] = func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		delete(f.validTok, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		f.mu.Unlock()
		w.WriteHeader(204)
	}
	n, err := c.RevokeTokens(bg)
	if err != nil || n != 1 || f.count("DELETE /installation/token") != 1 {
		t.Fatalf("revoked %d, err %v, deletes %d", n, err, f.count("DELETE /installation/token"))
	}
	f.mu.Lock()
	stillValid := f.validTok[first]
	f.mu.Unlock()
	if stillValid {
		t.Error("the revoked token still works")
	}
	second, err := c.InstallationToken(bg, "wstein/workharbor")
	if err != nil || second == first || f.mints != 2 {
		t.Errorf("after the revoke: token reused=%v mints=%d err=%v", second == first, f.mints, err)
	}
	// Nothing held, nothing to revoke.
	c2 := f.client(t, nil)
	if n, err := c2.RevokeTokens(bg); n != 0 || err != nil {
		t.Errorf("an idle client: %d, %v", n, err)
	}
}

func TestARevokeThatFailsIsReportedAndTheTokenIsStillForgotten(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	if _, err := c.InstallationToken(bg, "wstein/workharbor"); err != nil {
		t.Fatal(err)
	}
	f.handlers["DELETE /installation/token"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }
	n, err := c.RevokeTokens(bg)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "wstein/workharbor") {
		t.Errorf("revoked %d, err %v", n, err)
	}
	if _, err := c.InstallationToken(bg, "wstein/workharbor"); err != nil || f.mints != 2 {
		t.Errorf("a failed revoke must not keep the token: mints=%d err=%v", f.mints, err)
	}
}

func TestFastForwardNeverForcesAndSaysWhenTheBranchMoved(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	f.handlers["PATCH /repos/wstein/workharbor/git/refs/heads/develop"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]string{"ref": "refs/heads/develop"})
	}
	c := f.client(t, nil)
	sha := strings.Repeat("a", 40)
	if err := c.FastForward(bg, "wstein/workharbor", "develop", sha); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	body := f.bodies["PATCH /repos/wstein/workharbor/git/refs/heads/develop"]
	f.mu.Unlock()
	if !strings.Contains(body, `"force":false`) || !strings.Contains(body, sha) {
		t.Errorf("the request was %s: it must never force", body)
	}
	for _, bad := range []string{"", "not-a-sha", sha + "; drop"} {
		if err := c.FastForward(bg, "wstein/workharbor", "develop", bad); err == nil {
			t.Errorf("sha %q accepted", bad)
		}
	}
	if err := c.FastForward(bg, "wstein/workharbor", "a..b", sha); err == nil {
		t.Error("a bad branch accepted")
	}
	f.handlers["PATCH /repos/wstein/workharbor/git/refs/heads/develop"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 422, map[string]string{"message": "Update is not a fast forward"})
	}
	if err := c.FastForward(bg, "wstein/workharbor", "develop", sha); !errors.Is(err, forge.ErrNotFastForward) {
		t.Errorf("a branch that moved = %v, want ErrNotFastForward", err)
	}
	if err := c.FastForward(bg, "evil/other", "develop", sha); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("another repository = %v", err)
	}
}

func TestBranchRulesAndBypassActorsAreReadAndRefusalsAreUnreadable(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	f.handlers["GET /repos/wstein/workharbor/rules/branches/develop"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, []map[string]any{{"type": "pull_request", "ruleset_id": 5, "parameters": map[string]int{"required_approving_review_count": 1}}, {"type": "non_fast_forward", "ruleset_id": 5}})
	}
	f.handlers["GET /repos/wstein/workharbor/rulesets/5"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"bypass_actors": []map[string]any{{"actor_id": 1}, {"actor_id": 2}}})
	}
	f.handlers["GET /repos/wstein/workharbor/rulesets/6"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]any{"name": "no bypass list shown"})
	}
	f.handlers["GET /repos/wstein/workharbor/rulesets/7"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 403, map[string]string{"message": "Resource not accessible by integration"})
	}
	f.handlers["GET /repos/wstein/workharbor/rules/branches/main"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 403, map[string]string{"message": "Resource not accessible by integration"})
	}
	c := f.client(t, nil)
	rules, err := c.BranchRules(bg, "wstein/workharbor", "develop")
	if err != nil || len(rules) != 2 || rules[0].Type != "pull_request" || rules[0].RulesetID != 5 || !strings.Contains(string(rules[0].Parameters), "required_approving_review_count") {
		t.Fatalf("%+v, %v", rules, err)
	}
	if n, known, err := c.BypassActors(bg, "wstein/workharbor", 5); err != nil || !known || n != 2 {
		t.Errorf("bypass %d %v %v", n, known, err)
	}
	if _, known, err := c.BypassActors(bg, "wstein/workharbor", 6); err != nil || known {
		t.Errorf("a ruleset without a bypass list shown: known %v, %v", known, err)
	}
	if _, known, err := c.BypassActors(bg, "wstein/workharbor", 7); err != nil || known {
		t.Errorf("a refused ruleset read: known %v, %v", known, err)
	}
	if _, err := c.BranchRules(bg, "wstein/workharbor", "main"); !errors.Is(err, ErrRulesUnreadable) {
		t.Errorf("a refused read = %v, want ErrRulesUnreadable", err)
	}
	if _, err := c.BranchRules(bg, "wstein/workharbor", "a..b"); err == nil {
		t.Error("a bad branch accepted")
	}
}
