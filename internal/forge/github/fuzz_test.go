package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// FuzzGitHubAnswers lets GitHub answer the client's read and write calls with
// whatever status and body, as a hostile or broken server could. Invariants: no
// call panics or hangs; no error carries a token or an Authorization value; an issue
// that is returned is the one asked for and never a pull request; a pull request is
// opened only if the branch answer named exactly the approved commit; and a branch
// answer without a commit is an error, not an empty commit.
func FuzzGitHubAnswers(f *testing.F) {
	f.Add(uint16(200), []byte(`{"number":1,"title":"t","body":"b","author_association":"OWNER","user":{"login":"w"}}`))
	f.Add(uint16(200), []byte(`{"number":1,"pull_request":{"url":"x"}}`))
	f.Add(uint16(200), []byte(`{"object":{"sha":"aaaa"},"default_branch":"main","number":7,"html_url":"https://x/y"}`))
	f.Add(uint16(403), []byte(`{"message":"rate limit","documentation_url":"x"}`))
	f.Add(uint16(500), []byte(`<html>`))
	f.Add(uint16(200), []byte(`null`))
	f.Add(uint16(204), []byte(``))
	f.Add(uint16(200), []byte(`{"object":{"sha":""}}`))
	f.Fuzz(func(t *testing.T, status uint16, body []byte) {
		code := 100 + int(status)%500
		fake := newFake(t, func() time.Time { return time.Unix(1_700_000_000, 0) })
		answer := func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write(body)
		}
		for _, route := range []string{
			"GET /repos/wstein/workharbor/issues/1", "GET /repos/wstein/workharbor/git/ref/heads/agent/x",
			"GET /repos/wstein/workharbor", "POST /repos/wstein/workharbor/pulls",
		} {
			fake.handlers[route] = answer
		}
		c := fake.client(t, nil)
		check := func(what string, err error) {
			if err != nil && (strings.Contains(err.Error(), "ghs_installationtoken") || strings.Contains(strings.ToLower(err.Error()), "bearer ")) {
				t.Fatalf("%s: an error carries a credential: %v", what, err)
			}
		}

		issue, err := c.GetIssue(bg, "wstein/workharbor", 1)
		check("GetIssue", err)
		if err == nil {
			if issue.Repo != "wstein/workharbor" {
				t.Fatalf("the issue is for %q, not the repository asked", issue.Repo)
			}
			var probe struct {
				PullRequest json.RawMessage `json:"pull_request"`
			}
			if json.Unmarshal(body, &probe) == nil && len(probe.PullRequest) > 0 && string(probe.PullRequest) != "null" {
				t.Fatalf("a pull request was returned as an issue: %s", body)
			}
			if len(issue.Title)+len(issue.Body) > len(body) {
				t.Fatalf("the issue is larger than the answer it came from")
			}
		} else if code/100 == 2 {
			_ = errors.Is(err, ErrIsPR) // a 2xx error is a decoding error or a pull request
		}

		sha, err := c.BranchSHA(bg, "wstein/workharbor", "agent/x")
		check("BranchSHA", err)
		if err == nil && sha == "" {
			t.Fatal("an empty commit was returned for a branch")
		}

		const approved = "0123456789abcdef0123456789abcdef01234567"
		pr, err := c.OpenPR(bg, "wstein/workharbor", "agent/x", approved, "t", "b")
		check("OpenPR", err)
		if err == nil {
			var ref struct {
				Object struct {
					SHA string `json:"sha"`
				} `json:"object"`
			}
			if json.Unmarshal(body, &ref) != nil || ref.Object.SHA != approved {
				t.Fatalf("a pull request was opened although the branch answer was %q, not the approved commit", body)
			}
			if pr.SHA != approved || pr.Repo != "wstein/workharbor" {
				t.Fatalf("pull request = %+v", pr)
			}
		}
	})
}

// FuzzVerifyWebhook checks that a webhook is accepted only with the HMAC of its body
// under the secret, whatever header and body arrive, and never without a secret.
func FuzzVerifyWebhook(f *testing.F) {
	secret := []byte("webhook-secret-for-the-fuzz")
	sign := func(s, b []byte) string {
		m := hmac.New(sha256.New, s)
		m.Write(b)
		return "sha256=" + hex.EncodeToString(m.Sum(nil))
	}
	f.Add(secret, []byte(`{"action":"opened"}`), sign(secret, []byte(`{"action":"opened"}`)))
	f.Add(secret, []byte(`x`), "sha256=")
	f.Add(secret, []byte(`x`), "sha1=abcd")
	f.Add([]byte{}, []byte(`x`), sign(nil, []byte(`x`)))
	f.Add(secret, []byte(``), sign(secret, nil))
	f.Fuzz(func(t *testing.T, secret, body []byte, header string) {
		c := &Client{cfg: Config{WebhookSecret: secret}}
		h := http.Header{}
		h.Set("X-Hub-Signature-256", header)
		err := c.VerifyWebhook(h, body)
		if len(secret) == 0 && err == nil {
			t.Fatal("a webhook was accepted with no secret")
		}
		if err == nil && header != sign(secret, body) && !strings.EqualFold(header, sign(secret, body)) {
			t.Fatalf("a webhook was accepted with header %q, not the HMAC of its body", header)
		}
		if good := sign(secret, body); len(secret) > 0 && c.VerifyWebhook(headerOf(good), body) != nil {
			t.Fatal("a correct signature was refused")
		}
	})
}

func headerOf(sig string) http.Header {
	h := http.Header{}
	h.Set("X-Hub-Signature-256", sig)
	return h
}
