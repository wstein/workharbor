// Package github is the forge adapter for GitHub (design §10, D31). It calls
// the REST API as a GitHub App: a JWT signed with the App's private key is
// exchanged for an installation token scoped to one repository, and every call
// uses that token. It never shells out to gh and never uses a user token. The
// installation token is held in memory only, is registered with the redactor,
// and never reaches an environment (D18); the push is hostgit's job.
//
// It is standard library only. GraphQL (project board mirroring, issue #70) is
// not needed yet and is not here.
package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/redact"
)

// DefaultBaseURL is GitHub's API.
const DefaultBaseURL = "https://api.github.com"

// Errors the client returns, each matched with errors.Is. An API failure is
// also an *APIError with the status and GitHub's message.
var (
	ErrNotFound    = errors.New("github: not found")
	ErrAuth        = errors.New("github: the App is not authorised for this")
	ErrRateLimited = errors.New("github: rate limited")
	ErrNotAllowed  = errors.New("github: repository is not configured for this supervisor")
	ErrIsPR        = errors.New("github: the issue number is a pull request")
	ErrStale       = errors.New("github: the branch is not at the commit")
)

// APIError is an error answer from GitHub.
type APIError struct {
	Method, Path string
	Status       int
	Message      string
	// RetryAfter is when to try again, for a rate limit; zero otherwise.
	RetryAfter time.Time
	kind       error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

// Is makes errors.Is(err, ErrNotFound / ErrAuth / ErrRateLimited) work.
func (e *APIError) Is(target error) bool { return e.kind != nil && e.kind == target }

// Config is what the client needs.
type Config struct {
	AppID int64
	// Key is the App's private key (PEM, PKCS#1 or PKCS#8 RSA).
	Key *rsa.PrivateKey
	// Repos are the repositories ("owner/name") this supervisor works on: a
	// call about any other is refused before a request is made, and an
	// installation token is scoped to one of them.
	Repos []string
	// BaseURL defaults to DefaultBaseURL. Only https, or http to a loopback
	// address (a test server), is accepted.
	BaseURL string
	// HTTP is the client to use; default one with a 30 s timeout.
	HTTP *http.Client
	// Redactor, if set, learns each installation token the moment it is minted.
	Redactor *redact.Redactor
	// WebhookSecret verifies webhook signatures. Empty means webhooks are not
	// accepted.
	WebhookSecret []byte
	// Now is the clock; default time.Now.
	Now func() time.Time
}

// Client is the GitHub adapter. It implements forge.Adapter.
type Client struct {
	cfg  Config
	base *url.URL

	mu      sync.Mutex
	tokens  map[string]token  // by repository
	bases   map[string]string // default branch by repository
	install map[string]int64  // installation ID by repository
}

type token struct {
	value   string
	expires time.Time
}

// New returns a client.
func New(cfg Config) (*Client, error) {
	if cfg.AppID <= 0 {
		return nil, errors.New("github: an App ID is needed")
	}
	if cfg.Key == nil {
		return nil, errors.New("github: the App's private key is needed")
	}
	if len(cfg.Repos) == 0 {
		return nil, errors.New("github: at least one repository is needed")
	}
	for _, r := range cfg.Repos {
		if !hostgit.ValidRepoName(r) {
			return nil, fmt.Errorf("github: %q is not owner/name", r)
		}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("github: %q is not a usable base URL", cfg.BaseURL)
	}
	switch base.Scheme {
	case "https":
	case "http":
		host, _, herr := net.SplitHostPort(base.Host)
		if herr != nil {
			host = base.Host
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return nil, errors.New("github: the base URL must be https (http only to a loopback address, for tests)")
		}
	default:
		return nil, errors.New("github: the base URL must be https")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Client{cfg: cfg, base: base, tokens: map[string]token{}, bases: map[string]string{}, install: map[string]int64{}}, nil
}

// ParsePrivateKey reads an RSA private key in PEM, PKCS#1 or PKCS#8. The error
// never contains the key.
func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("github: the key file holds no PEM block")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("github: the key is neither a PKCS#1 nor a PKCS#8 private key")
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github: the App key must be an RSA key")
	}
	return rk, nil
}

// Name implements forge.Adapter.
func (c *Client) Name() string { return "github" }

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// appJWT signs the short-lived JWT that authenticates as the App itself. It is
// valid for nine minutes and backdated by a minute for clock drift, as GitHub
// asks.
func (c *Client) appJWT() (string, error) {
	now := c.cfg.Now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": c.cfg.AppID})
	signing := b64(header) + "." + b64(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.cfg.Key, crypto.SHA256, sum[:])
	if err != nil {
		return "", errors.New("github: signing the App JWT failed")
	}
	return signing + "." + b64(sig), nil
}

func (c *Client) allowed(repo string) error {
	for _, r := range c.cfg.Repos {
		if strings.EqualFold(r, repo) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotAllowed, repo)
}

// do sends one request with a bearer credential and decodes the answer into
// out (if not nil). A failure is an *APIError; the credential is never in it.
func (c *Client) do(ctx context.Context, bearer, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "workharbor")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // the URL is not needed and a request error must never carry a header
		}
		return fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	if resp.StatusCode/100 != 2 {
		return c.apiError(method, path, resp, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("github: %s %s: the answer is not what was expected: %w", method, path, err)
		}
	}
	return nil
}

func (c *Client) apiError(method, path string, resp *http.Response, raw []byte) error {
	var m struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &m)
	msg := m.Message
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	if c.cfg.Redactor != nil {
		msg = c.cfg.Redactor.String(msg)
	}
	e := &APIError{Method: method, Path: path, Status: resp.StatusCode, Message: oneLine(msg)}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")):
		e.kind = ErrRateLimited
		if v := resp.Header.Get("Retry-After"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				e.RetryAfter = c.cfg.Now().Add(time.Duration(n) * time.Second)
			}
		} else if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				e.RetryAfter = time.Unix(n, 0)
			}
		}
	case resp.StatusCode == http.StatusNotFound:
		e.kind = ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		e.kind = ErrAuth
	}
	return e
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// installationToken returns a token for the App's installation on repo, scoped
// to that one repository and to the permissions the supervisor needs: issues
// and pull requests (comments, PRs) and contents (the push, which hostgit makes
// on the host). It is cached until a minute before it expires and registered
// with the redactor.
func (c *Client) installationToken(ctx context.Context, repo string) (string, error) {
	if err := c.allowed(repo); err != nil {
		return "", err
	}
	key := strings.ToLower(repo)
	c.mu.Lock()
	if t, ok := c.tokens[key]; ok && c.cfg.Now().Add(time.Minute).Before(t.expires) {
		c.mu.Unlock()
		return t.value, nil
	}
	id := c.install[key]
	c.mu.Unlock()

	jwt, err := c.appJWT()
	if err != nil {
		return "", err
	}
	if id == 0 {
		var inst struct {
			ID int64 `json:"id"`
		}
		if err := c.do(ctx, jwt, http.MethodGet, "/repos/"+repo+"/installation", nil, &inst); err != nil {
			return "", fmt.Errorf("find the App's installation on %s: %w", repo, err)
		}
		id = inst.ID
	}
	_, name, _ := strings.Cut(repo, "/")
	var tok struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	err = c.do(ctx, jwt, http.MethodPost, "/app/installations/"+strconv.FormatInt(id, 10)+"/access_tokens", map[string]any{
		"repositories": []string{name},
		"permissions":  map[string]string{"issues": "write", "pull_requests": "write", "contents": "write", "metadata": "read"},
	}, &tok)
	if err != nil {
		return "", fmt.Errorf("mint an installation token for %s: %w", repo, err)
	}
	if tok.Token == "" || tok.ExpiresAt.IsZero() {
		return "", errors.New("github: the installation token answer is incomplete")
	}
	if c.cfg.Redactor != nil {
		c.cfg.Redactor.Add(tok.Token)
	}
	c.mu.Lock()
	c.install[key] = id
	c.tokens[key] = token{value: tok.Token, expires: tok.ExpiresAt}
	c.mu.Unlock()
	return tok.Token, nil
}

// call makes an installation-authenticated request for repo, retrying once with
// a fresh token if the cached one was refused.
func (c *Client) call(ctx context.Context, repo, method, path string, body, out any) error {
	tok, err := c.installationToken(ctx, repo)
	if err != nil {
		return err
	}
	err = c.do(ctx, tok, method, path, body, out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		c.mu.Lock()
		delete(c.tokens, strings.ToLower(repo)) // revoked or expired early: mint again
		c.mu.Unlock()
		if tok, err = c.installationToken(ctx, repo); err != nil {
			return err
		}
		err = c.do(ctx, tok, method, path, body, out)
	}
	return err
}

// InstallationToken returns a current installation token for repo. The host
// uses it for the push and nothing else; it must never be passed to an
// environment (D18). The caller must not log it.
func (c *Client) InstallationToken(ctx context.Context, repo string) (string, error) {
	return c.installationToken(ctx, repo)
}

// GetIssue implements forge.Adapter. A pull request is not an issue.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (forge.Issue, error) {
	if number <= 0 {
		return forge.Issue{}, fmt.Errorf("github: issue number %d", number)
	}
	var v struct {
		Number            int             `json:"number"`
		Title             string          `json:"title"`
		Body              string          `json:"body"`
		AuthorAssociation string          `json:"author_association"`
		PullRequest       json.RawMessage `json:"pull_request"`
		User              struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.call(ctx, repo, http.MethodGet, "/repos/"+repo+"/issues/"+strconv.Itoa(number), nil, &v); err != nil {
		return forge.Issue{}, err
	}
	if len(v.PullRequest) > 0 && string(v.PullRequest) != "null" {
		return forge.Issue{}, fmt.Errorf("%w: %s#%d", ErrIsPR, repo, number)
	}
	return forge.Issue{Repo: repo, Number: v.Number, Title: v.Title, Body: v.Body, Author: v.User.Login, AuthorAssociation: v.AuthorAssociation}, nil
}

// CommentIssue implements forge.Adapter.
func (c *Client) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	if number <= 0 || strings.TrimSpace(body) == "" {
		return errors.New("github: a comment needs an issue number and a text")
	}
	return c.call(ctx, repo, http.MethodPost, "/repos/"+repo+"/issues/"+strconv.Itoa(number)+"/comments", map[string]string{"body": body}, nil)
}

func escapeBranch(branch string) (string, error) {
	if branch == "" || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.Contains(branch, "//") || strings.Contains(branch, "..") {
		return "", fmt.Errorf("github: %q is not a branch name", branch)
	}
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/"), nil
}

// BranchSHA implements forge.Adapter: the commit a branch points at on GitHub.
// A branch that does not exist is ErrNotFound.
func (c *Client) BranchSHA(ctx context.Context, repo, branch string) (string, error) {
	esc, err := escapeBranch(branch)
	if err != nil {
		return "", err
	}
	var v struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.call(ctx, repo, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+esc, nil, &v); err != nil {
		return "", err
	}
	if v.Object.SHA == "" {
		return "", errors.New("github: the branch answer has no commit")
	}
	return v.Object.SHA, nil
}

func (c *Client) defaultBranch(ctx context.Context, repo string) (string, error) {
	key := strings.ToLower(repo)
	c.mu.Lock()
	b, ok := c.bases[key]
	c.mu.Unlock()
	if ok {
		return b, nil
	}
	var v struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.call(ctx, repo, http.MethodGet, "/repos/"+repo, nil, &v); err != nil {
		return "", err
	}
	if v.DefaultBranch == "" {
		return "", errors.New("github: the repository answer has no default branch")
	}
	c.mu.Lock()
	c.bases[key] = v.DefaultBranch
	c.mu.Unlock()
	return v.DefaultBranch, nil
}

type prAnswer struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// OpenPR implements forge.Adapter: it opens a pull request from branch into the
// default branch, after checking that the branch is at sha on GitHub. The
// forge.Guard checks the approval before this is called; this check is the
// adapter's own, so a branch moved after the approval is not published.
func (c *Client) OpenPR(ctx context.Context, repo, branch, sha, title, body string) (forge.PullRequest, error) {
	if err := c.checkBranch(ctx, repo, branch, sha); err != nil {
		return forge.PullRequest{}, err
	}
	base, err := c.defaultBranch(ctx, repo)
	if err != nil {
		return forge.PullRequest{}, err
	}
	var v prAnswer
	err = c.call(ctx, repo, http.MethodPost, "/repos/"+repo+"/pulls", map[string]any{"title": title, "head": branch, "base": base, "body": body}, &v)
	if err != nil {
		return forge.PullRequest{}, err
	}
	return forge.PullRequest{Repo: repo, Number: v.Number, URL: v.HTMLURL, Branch: branch, SHA: sha}, nil
}

// UpdatePR implements forge.Adapter: it updates the title and body of a pull
// request whose branch is now at sha.
func (c *Client) UpdatePR(ctx context.Context, pr forge.PullRequest, sha, title, body string) error {
	if pr.Number <= 0 {
		return errors.New("github: a pull request needs a number")
	}
	if err := c.checkBranch(ctx, pr.Repo, pr.Branch, sha); err != nil {
		return err
	}
	return c.call(ctx, pr.Repo, http.MethodPatch, "/repos/"+pr.Repo+"/pulls/"+strconv.Itoa(pr.Number), map[string]string{"title": title, "body": body}, nil)
}

func (c *Client) checkBranch(ctx context.Context, repo, branch, sha string) error {
	got, err := c.BranchSHA(ctx, repo, branch)
	if err != nil {
		return err
	}
	if got != sha {
		return fmt.Errorf("%w: %s is at %s, not %s", ErrStale, branch, short(got), short(sha))
	}
	return nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// VerifyWebhook implements forge.Adapter: it checks X-Hub-Signature-256, an
// HMAC-SHA256 of the body with the webhook secret, in constant time.
func (c *Client) VerifyWebhook(h http.Header, body []byte) error {
	if len(c.cfg.WebhookSecret) == 0 {
		return errors.New("github: no webhook secret is configured: webhooks are not accepted")
	}
	got, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256=")
	if !ok {
		return errors.New("github: the webhook has no sha256 signature")
	}
	sig, err := hex.DecodeString(got)
	if err != nil {
		return errors.New("github: the webhook signature is not hex")
	}
	mac := hmac.New(sha256.New, c.cfg.WebhookSecret)
	mac.Write(body)
	if subtle.ConstantTimeCompare(mac.Sum(nil), sig) != 1 {
		return errors.New("github: the webhook signature does not match")
	}
	return nil
}

var _ forge.Adapter = (*Client)(nil)
