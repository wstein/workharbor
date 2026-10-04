package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/policy"
	"github.com/wstein/workharbor/internal/redact"
)

// stubGit records what the Pusher hands to hostgit.
type stubGit struct {
	remote, branch, sha, token string
	err                        error
	check                      func(token string) // runs while the push is in flight
}

func (s *stubGit) PushToken(_ context.Context, remote, branch, sha, token string) error {
	s.remote, s.branch, s.sha, s.token = remote, branch, sha, token
	if s.check != nil {
		s.check(token)
	}
	return s.err
}

func (f *fakeGitHub) revokeHandler() {
	f.handlers["DELETE /installation/token"] = func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		delete(f.validTok, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		f.mu.Unlock()
		w.WriteHeader(204)
	}
}

func (f *fakeGitHub) valid(tok string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.validTok[tok]
}

func TestPushMintsAScopedTokenHandsItToGitAndRevokesIt(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	f.revokeHandler()
	red := redact.New(redact.WithoutDefaults())
	c := f.client(t, func(cfg *Config) { cfg.Redactor = red; cfg.GitBaseURL = "https://git.example.test" })
	sha := strings.Repeat("a", 40)
	g := &stubGit{}
	g.check = func(tok string) {
		if !f.valid(tok) {
			t.Error("the token was not usable during the push")
		}
	}
	if err := c.NewPusher(g).Push(bg, "wstein/workharbor", "agent/docs", sha); err != nil {
		t.Fatal(err)
	}
	if g.remote != "https://git.example.test/wstein/workharbor.git" || g.branch != "agent/docs" || g.sha != sha {
		t.Errorf("git got %q %q %q", g.remote, g.branch, g.sha)
	}
	if strings.Contains(g.remote, g.token) {
		t.Error("the token is in the URL")
	}
	var req struct {
		Repositories []string          `json:"repositories"`
		Permissions  map[string]string `json:"permissions"`
	}
	f.mu.Lock()
	_ = json.Unmarshal([]byte(f.bodies["POST /app/installations/7/access_tokens"]), &req)
	f.mu.Unlock()
	if len(req.Repositories) != 1 || req.Repositories[0] != "workharbor" || len(req.Permissions) != 1 || req.Permissions["contents"] != "write" {
		t.Errorf("the token was asked for as %+v", req)
	}
	if f.count("DELETE /installation/token") != 1 || f.valid(g.token) {
		t.Error("the push token was not revoked")
	}
	if got := red.String("x " + g.token); strings.Contains(got, g.token) {
		t.Error("the token was not registered with the redactor")
	}
	// every push mints its own
	if err := c.NewPusher(g).Push(bg, "wstein/workharbor", "agent/docs", sha); err != nil || f.mints != 2 {
		t.Errorf("second push: mints=%d err=%v", f.mints, err)
	}
}

func TestPushRevokesOnEveryFailurePathAndKeepsTheTokenOutOfErrors(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	f.revokeHandler()
	c := f.client(t, nil)
	sha := strings.Repeat("b", 40)

	// git fails with the token in its text, as a careless message could
	g := &stubGit{}
	g.check = func(tok string) { g.err = fmt.Errorf("%w: remote said no for %s", hostgit.ErrNotFastForward, tok) }
	err := c.NewPusher(g).Push(bg, "wstein/workharbor", "agent/docs", sha)
	if err == nil || strings.Contains(err.Error(), g.token) {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, hostgit.ErrNotFastForward) {
		t.Errorf("the sentinel was lost: %v", err)
	}
	if f.count("DELETE /installation/token") != 1 || f.valid(g.token) {
		t.Error("a failed push did not revoke the token")
	}

	// a cancelled context still revokes
	ctx, cancel := context.WithCancel(bg)
	g2 := &stubGit{}
	g2.check = func(string) { cancel(); g2.err = ctx.Err() }
	if err := c.NewPusher(g2).Push(ctx, "wstein/workharbor", "agent/docs", sha); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if f.count("DELETE /installation/token") != 2 || f.valid(g2.token) {
		t.Error("a cancelled push did not revoke the token")
	}

	// a revoke that fails is reported, and a token already gone is not an error
	f.handlers["DELETE /installation/token"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }
	if err := c.NewPusher(&stubGit{}).Push(bg, "wstein/workharbor", "agent/docs", sha); err == nil || !strings.Contains(err.Error(), "revoke") {
		t.Errorf("a failed revoke = %v", err)
	}
	f.handlers["DELETE /installation/token"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }
	if err := c.NewPusher(&stubGit{}).Push(bg, "wstein/workharbor", "agent/docs", sha); err != nil {
		t.Errorf("a token that is already gone = %v", err)
	}

	// another repository is refused before any token is minted
	before := f.mints
	if err := c.NewPusher(&stubGit{}).Push(bg, "evil/other", "agent/docs", sha); !errors.Is(err, ErrNotAllowed) || f.mints != before {
		t.Errorf("another repository = %v (mints %d -> %d)", err, before, f.mints)
	}
	// a token that cannot be minted never reaches git
	f.handlers["POST /app/installations/7/access_tokens"] = func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 422, map[string]string{"message": "no"})
	}
	g3 := &stubGit{}
	if err := c.NewPusher(g3).Push(bg, "wstein/workharbor", "agent/docs", sha); err == nil || g3.remote != "" {
		t.Errorf("a failed mint = %v, git called with %q", err, g3.remote)
	}
}

func TestTheGitBaseURLMustBeHTTPS(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	for _, bad := range []string{"http://github.com", "ssh://git@github.com", "https://u:p@github.com", "https://github.com?x=1", "file:///x"} {
		_, err := New(Config{AppID: 4242, Key: key(t), Repos: []string{"wstein/workharbor"}, BaseURL: f.ts.URL, GitBaseURL: bad})
		if err == nil {
			t.Errorf("git base %q accepted", bad)
		}
	}
}

func TestOpenPRReturnsTheOwnOpenPRInsteadOfOpeningASecond(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	c := f.client(t, nil)
	f.handlers["GET /repos/wstein/workharbor/git/ref/heads/agent/docs"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"object":{"sha":"approved1"}}`)
	}
	f.handlers["GET /repos/wstein/workharbor"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"default_branch":"main"}`)
	}
	var query string
	list := `[{"number":3,"html_url":"u3","head":{"sha":"x","ref":"agent/docs","repo":{"full_name":"fork/workharbor"}},"base":{"ref":"main"}},{"number":4,"html_url":"u4","head":{"sha":"x","ref":"agent/docs","repo":{"full_name":"wstein/workharbor"}},"base":{"ref":"other"}},{"number":5,"html_url":"u5","head":{"sha":"old","ref":"agent/docs","repo":{"full_name":"Wstein/Workharbor"}},"base":{"ref":"main"}}]`
	f.handlers["GET /repos/wstein/workharbor/pulls"] = func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = io.WriteString(w, list)
	}
	f.handlers["POST /repos/wstein/workharbor/pulls"] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"number":9,"html_url":"u9"}`)
	}
	f.handlers["PATCH /repos/wstein/workharbor/pulls/5"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{}") }
	pr, err := c.OpenPR(bg, "wstein/workharbor", "agent/docs", "approved1", "revision 2", "summary 2")
	if err != nil || pr.Number != 5 || pr.URL != "u5" || pr.SHA != "approved1" || pr.Branch != "agent/docs" {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	// the found PR gets the new revision's title and body
	var patched map[string]string
	f.mu.Lock()
	_ = json.Unmarshal([]byte(f.bodies["PATCH /repos/wstein/workharbor/pulls/5"]), &patched)
	f.mu.Unlock()
	if patched["title"] != "revision 2" || patched["body"] != "summary 2" {
		t.Errorf("the found PR was patched with %v", patched)
	}
	if f.count("POST /repos/wstein/workharbor/pulls") != 0 {
		t.Error("a second pull request was opened")
	}
	for _, want := range []string{"state=open", "head=wstein%3Aagent%2Fdocs", "base=main"} {
		if !strings.Contains(query, want) {
			t.Errorf("the lookup %q lacks %s", query, want)
		}
	}
	// only a fork's PR and another base: none of them is ours, so one is opened
	list = `[{"number":3,"html_url":"u3","head":{"sha":"x","ref":"agent/docs","repo":{"full_name":"fork/workharbor"}},"base":{"ref":"main"}}]`
	pr, err = c.OpenPR(bg, "wstein/workharbor", "agent/docs", "approved1", "t", "b")
	if err != nil || pr.Number != 9 {
		t.Fatalf("pr = %+v, %v", pr, err)
	}
	// a lookup that fails is an error, never a second PR
	f.handlers["GET /repos/wstein/workharbor/pulls"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }
	posts := f.count("POST /repos/wstein/workharbor/pulls")
	if _, err := c.OpenPR(bg, "wstein/workharbor", "agent/docs", "approved1", "t", "b"); err == nil || f.count("POST /repos/wstein/workharbor/pulls") != posts {
		t.Errorf("a failed lookup = %v", err)
	}
}

func TestFastForwardToTheCommitTheTargetAlreadyHasSucceedsWithoutAWrite(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	sha := strings.Repeat("c", 40)
	f.handlers["GET /repos/wstein/workharbor/git/ref/heads/develop"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"object":{"sha":"`+sha+`"}}`)
	}
	c := f.client(t, nil)
	if err := c.FastForward(bg, "wstein/workharbor", "develop", sha); err != nil {
		t.Fatal(err)
	}
	if f.count("PATCH /repos/wstein/workharbor/git/refs/heads/develop") != 0 {
		t.Error("the branch was written although it was already there")
	}
}

func TestBotIdentityIsReadFromTheApp(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	f.handlers["GET /app"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"slug":"workharbor-bot"}`) }
	f.handlers["GET /users/workharbor-bot[bot]"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"id":123456}`) }
	c := f.client(t, nil)
	id, err := c.BotIdentity(bg)
	if err != nil || id.Name != "workharbor-bot[bot]" || id.Email != "123456+workharbor-bot[bot]@users.noreply.github.com" {
		t.Fatalf("identity = %+v, %v", id, err)
	}
	f.handlers["GET /app"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"slug":"a b\n<x>"}`) }
	if _, err := c.BotIdentity(bg); err == nil {
		t.Error("a slug with odd characters was accepted")
	}
	f.handlers["GET /app"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"slug":"workharbor-bot"}`) }
	f.handlers["GET /users/workharbor-bot[bot]"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{}`) }
	if _, err := c.BotIdentity(bg); err == nil {
		t.Error("a bot without an ID was accepted")
	}
}

var _ forge.Pusher = (*Pusher)(nil)

func TestOnlyAnApprovedPushThroughTheGuardMintsAToken(t *testing.T) {
	now, _ := clock()
	f := newFake(t, now)
	f.revokeHandler()
	c := f.client(t, nil)
	sha := strings.Repeat("d", 40)
	g := &stubGit{}
	guard := forge.NewGuard(c, c.NewPusher(g), policy.Default(), approvedOnly{"d1", sha})
	if err := guard.Push(bg, "wstein/workharbor", "agent/docs", forge.Approval{DecisionID: "d1", SHA: strings.Repeat("e", 40)}); !errors.Is(err, forge.ErrNotApproved) {
		t.Errorf("an unapproved commit = %v", err)
	}
	if err := guard.Push(bg, "wstein/workharbor", "main", forge.Approval{DecisionID: "d1", SHA: sha}); !errors.Is(err, forge.ErrBranch) {
		t.Errorf("a non-agent branch = %v", err)
	}
	if f.mints != 0 || g.remote != "" {
		t.Fatalf("a refused push minted %d tokens and called git with %q", f.mints, g.remote)
	}
	if err := guard.Push(bg, "wstein/workharbor", "agent/docs", forge.Approval{DecisionID: "d1", SHA: sha}); err != nil || f.mints != 1 {
		t.Errorf("an approved push = %v, mints %d", err, f.mints)
	}
}

func TestAPIErrorTransientMatchesRateLimitsAndServerErrors(t *testing.T) {
	for _, tc := range []struct {
		err  *APIError
		want bool
	}{
		{&APIError{Status: 502}, true},
		{&APIError{Status: 429, kind: ErrRateLimited}, true},
		{&APIError{Status: 403, kind: ErrAuth}, false},
		{&APIError{Status: 404, kind: ErrNotFound}, false},
		{&APIError{Status: 422}, false},
	} {
		if got := errors.Is(tc.err, forge.ErrTransient); got != tc.want {
			t.Errorf("status %d: transient = %v, want %v", tc.err.Status, got, tc.want)
		}
	}
}
