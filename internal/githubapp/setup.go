// Package githubapp creates the operator's own GitHub App through GitHub's
// manifest flow (design §9.5, D15, D31): no manual App creation and no .pem
// download. A short-lived server answers the redirect GitHub sends the browser
// to; the only things it accepts are a `state` the create command minted, once.
package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/forge/github"
	"github.com/wstein/workharbor/internal/redact"
)

// Paths of the two pages. Both need a live state; nothing else is served.
const (
	StartPath    = "/github/app/new"
	CallbackPath = "/github/app/callback"
)

var (
	nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,33}$`)
	orgRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
)

// Config is what a setup needs.
type Config struct {
	// PublicURL is whr's HTTPS name behind the forwarder (D29), without a path:
	// GitHub redirects the browser to it.
	PublicURL string
	// Name is the App's name (GitHub App names are unique and at most 34
	// characters).
	Name string
	// Board adds the project board's permission to the manifest (D30).
	Board bool
	// Org, if set, creates the App in that organization instead of the
	// operator's account.
	Org string
	// KeyDir is where the private key is written (0600, exclusive create).
	KeyDir string
	// GitHubURL is the web address of GitHub (default https://github.com) and
	// APIURL its API (default https://api.github.com). Only https, or http to a
	// loopback address (a test double).
	GitHubURL, APIURL string
	TTL               time.Duration
	HTTP              *http.Client
	Redactor          *redact.Redactor
	Now               func() time.Time
}

// Result is what a finished setup tells the operator. It holds no secret.
type Result struct {
	AppID      int64  `json:"app_id"`
	Slug       string `json:"slug"`
	KeyFile    string `json:"key_file"`
	InstallURL string `json:"install_url"`
}

// Setup is one run of the manifest flow.
type Setup struct {
	cfg     Config
	public  *url.URL
	web     *url.URL
	api     *url.URL
	states  *States
	results chan Result
}

// NewSetup checks cfg and returns a setup.
func NewSetup(cfg Config) (*Setup, error) {
	public, err := CheckBaseURL(cfg.PublicURL)
	if err != nil {
		return nil, fmt.Errorf("public URL: %w", err)
	}
	if strings.Trim(public.Path, "/") != "" {
		return nil, fmt.Errorf("public URL %q: give the name only, without a path", cfg.PublicURL)
	}
	if cfg.GitHubURL == "" {
		cfg.GitHubURL = "https://github.com"
	}
	if cfg.APIURL == "" {
		cfg.APIURL = github.DefaultBaseURL
	}
	web, err := CheckBaseURL(cfg.GitHubURL)
	if err != nil {
		return nil, fmt.Errorf("GitHub URL: %w", err)
	}
	api, err := CheckBaseURL(cfg.APIURL)
	if err != nil {
		return nil, fmt.Errorf("GitHub API URL: %w", err)
	}
	if !nameRE.MatchString(cfg.Name) {
		return nil, fmt.Errorf("the App name %q: 1 to 34 letters, digits, spaces, '_' and '-'", cfg.Name)
	}
	if cfg.Org != "" && !orgRE.MatchString(cfg.Org) {
		return nil, fmt.Errorf("organization %q is not a GitHub organization name", cfg.Org)
	}
	if cfg.KeyDir == "" {
		return nil, errors.New("a key directory is needed")
	}
	if cfg.Redactor == nil {
		cfg.Redactor = redact.New()
	}
	return &Setup{cfg: cfg, public: public, web: web, api: api, states: NewStates(cfg.TTL, cfg.Now), results: make(chan Result, 1)}, nil
}

// StartURL mints a state and returns the address that, opened in a browser,
// posts the manifest to GitHub, and when the state expires.
func (s *Setup) StartURL() (string, time.Time) {
	st, exp := s.states.Mint(s.cfg.Org)
	u := *s.public
	u.Path = StartPath
	u.RawQuery = url.Values{"state": {st}}.Encode()
	return u.String(), exp
}

// Wait returns the result once the redirect has been handled, or an error when
// ctx ends first.
func (s *Setup) Wait(ctx context.Context) (Result, error) {
	select {
	case r := <-s.results:
		return r, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Manifest is the App manifest (design D15, D31): a private App with no
// webhook, no events and exactly github.AppPermissions. The hook URL is a
// placeholder that is never called, because the hook is inactive; whether
// GitHub accepts a manifest without one is unverified.
func (s *Setup) Manifest() map[string]any {
	redirect := *s.public
	redirect.Path = CallbackPath
	hook := *s.public
	hook.Path = "/github/app/hook"
	return map[string]any{
		"name":                s.cfg.Name,
		"url":                 s.public.String(),
		"hook_attributes":     map[string]any{"url": hook.String(), "active": false},
		"redirect_url":        redirect.String(),
		"public":              false,
		"default_permissions": github.AppPermissionsFor(s.cfg.Board),
		"default_events":      []string{},
	}
}

// Handler serves the start page and the redirect and nothing else: every other
// path, method or state is a 4xx that changes nothing.
func (s *Setup) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+StartPath, s.start)
	mux.HandleFunc("GET "+CallbackPath, s.callback)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action "+s.web.Scheme+"://"+s.web.Host)
		if _, pattern := mux.Handler(r); pattern == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func refuse(w http.ResponseWriter) {
	http.Error(w, "this link is not valid, has expired or was already used: run `whr github app create` again", http.StatusBadRequest)
}

var startPage = template.Must(template.New("start").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Create the workharbor GitHub App</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:34em;margin:2em auto;padding:0 16px}button{font:inherit;padding:.6em 1em}</style></head>
<body><h1>Create the workharbor GitHub App</h1>
<p>This creates a private GitHub App named <b>{{.Name}}</b>{{if .Org}} in the organization <b>{{.Org}}</b>{{end}} with only the permissions workharbor needs. GitHub asks you to confirm.</p>
<form method="post" action="{{.Action}}"><input type="hidden" name="manifest" value="{{.Manifest}}"><button type="submit">Continue to GitHub</button></form>
</body></html>`))

func (s *Setup) start(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	org, ok := s.states.Peek(state)
	if !ok {
		refuse(w)
		return
	}
	manifest, err := json.Marshal(s.Manifest())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	action := *s.web
	if org != "" {
		action.Path = "/organizations/" + org + "/settings/apps/new"
	} else {
		action.Path = "/settings/apps/new"
	}
	action.RawQuery = url.Values{"state": {state}}.Encode()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = startPage.Execute(w, map[string]any{"Name": s.cfg.Name, "Org": org, "Action": action.String(), "Manifest": string(manifest)})
}

var donePage = template.Must(template.New("done").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>GitHub App created</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:34em;margin:2em auto;padding:0 16px}code{overflow-wrap:anywhere}</style></head>
<body><h1>The GitHub App is created</h1>
<p>Back in your terminal, <code>whr github app create</code> prints what to put in the configuration. The private key was saved on the host; it is not shown here.</p>
<p>Next: <a href="{{.InstallURL}}" rel="noreferrer">install the App on your selected repositories</a>, and check that the main branch's ruleset does not list the App as a bypass actor.</p>
</body></html>`))

func (s *Setup) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, ok := s.states.Take(q.Get("state")); !ok { // spent here: a replay finds nothing
		refuse(w)
		return
	}
	code := q.Get("code")
	if !ValidCode(code) {
		http.Error(w, "the redirect carried no valid code", http.StatusBadRequest)
		return
	}
	conv, err := Convert(r.Context(), s.cfg.HTTP, s.api, code, s.cfg.Redactor)
	if err != nil {
		http.Error(w, s.cfg.Redactor.String(err.Error()), http.StatusBadGateway)
		return
	}
	path, err := WriteKey(s.cfg.KeyDir, conv.ID, conv.PEM)
	if err != nil {
		// The App exists on GitHub but its key could not be stored; the key is
		// not in the answer, so say what to do.
		http.Error(w, s.cfg.Redactor.String(err.Error())+": delete the App on GitHub and run the setup again", http.StatusInternalServerError)
		return
	}
	if _, err := config.ReadSecret(path); err != nil {
		http.Error(w, "the key was saved but whr would refuse it: "+s.cfg.Redactor.String(err.Error()), http.StatusInternalServerError)
		return
	}
	install := *s.web
	install.Path = "/apps/" + conv.Slug + "/installations/new"
	res := Result{AppID: conv.ID, Slug: conv.Slug, KeyFile: path, InstallURL: install.String()}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = donePage.Execute(w, res)
	select {
	case s.results <- res:
	default:
	}
}
