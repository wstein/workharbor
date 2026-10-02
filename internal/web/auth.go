// Package web is the web UI (design §9.3, D8): `templ` pages rendered by the Go
// server, htmx for partial updates and SSE for the live transcript. Its handlers
// are thin: they call the same service layer as the JSON API.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Session is who a request is, as far as a handler needs to know. CSRF is the
// token every POST of the session must carry.
type Session struct {
	CSRF string
	// ID names the session among the signed-in devices (see Devices); it is not
	// the cookie and cannot be used to act as the session.
	ID string
}

// Auth is everything the UI needs of authentication, so that the passkeys of
// D45 replace the token without touching a handler.
type Auth interface {
	// Session returns the session of a request, if it has a valid one.
	Session(r *http.Request) (Session, bool)
	// SignIn checks the sign-in form of r and, if it is right, starts a session
	// and sets its cookie.
	SignIn(w http.ResponseWriter, r *http.Request) error
	// SignOut ends the session of r and clears its cookie.
	SignOut(w http.ResponseWriter, r *http.Request)
}

// Starter is an Auth that can begin a session for a request that was verified by
// other means, as the passkey sign-in does.
type Starter interface {
	Start(w http.ResponseWriter, r *http.Request)
}

// Device is a signed-in browser as the device list shows it.
type Device struct {
	ID       string
	Label    string // a phone, a tablet or a computer and its browser, from the User-Agent
	Since    time.Time
	LastSeen time.Time
	Current  bool
}

// Devices is an Auth that can list its sessions and end one, so a lost phone is
// signed out from the tablet (D35). The sessions are per device, not per token:
// the API token stays one secret for the host's CLI.
type Devices interface {
	Devices(r *http.Request) []Device
	// Revoke ends the session with the ID and reports whether there was one.
	Revoke(id string) bool
}

// Errors of signing in.
var (
	ErrBadCredentials = errors.New("that is not the token")
	ErrTooManyTries   = errors.New("too many failed tries: wait a minute")
)

const (
	cookieName = "whr_session"
	sessionTTL = 12 * time.Hour
	// A phone is the device a thief or a child picks up, so a session on one ends
	// after a short idle time (D35); a tablet or a computer keeps one longer.
	phoneIdle     = 15 * time.Minute
	desktopIdle   = 8 * time.Hour
	maxFailures   = 5
	failureWindow = time.Minute
)

// TokenAuth signs in with the API token: the human pastes it once, and a random
// session ID in a cookie stands in for it from then on. Sessions are kept in
// memory, so a restart signs everyone out.
type TokenAuth struct {
	digest [sha256.Size]byte
	now    func() time.Time

	mu       sync.Mutex
	sessions map[[sha256.Size]byte]tokenSession // by the hash of the cookie value
	failures []time.Time
}

type tokenSession struct {
	csrf     string
	expires  time.Time
	lastSeen time.Time
	since    time.Time
	idle     time.Duration
	label    string
	id       string
}

// NewTokenAuth returns the token authentication. now may be nil.
func NewTokenAuth(token []byte, now func() time.Time) (*TokenAuth, error) {
	if len(token) == 0 {
		return nil, errors.New("web: a token is required")
	}
	if now == nil {
		now = time.Now
	}
	return &TokenAuth{digest: sha256.Sum256(token), now: now, sessions: map[[sha256.Size]byte]tokenSession{}}, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("web: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Session implements Auth.
func (a *TokenAuth) Session(r *http.Request) (Session, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return Session{}, false
	}
	key := sha256.Sum256([]byte(c.Value))
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[key]
	now := a.now()
	if !ok || !now.Before(s.expires) || now.Sub(s.lastSeen) > s.idle {
		delete(a.sessions, key)
		return Session{}, false
	}
	s.lastSeen = now
	a.sessions[key] = s
	return Session{CSRF: s.csrf, ID: s.id}, true
}

// SignIn implements Auth. The token is compared in constant time through its
// hash, a wrong one is slowed down by a limit on failures, and the cookie is
// HttpOnly, SameSite=Strict, and Secure when the request came over HTTPS (TLS,
// or the forwarder saying so).
func (a *TokenAuth) SignIn(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return ErrBadCredentials
	}
	now := a.now()
	a.mu.Lock()
	recent := a.failures[:0]
	for _, t := range a.failures {
		if now.Sub(t) < failureWindow {
			recent = append(recent, t)
		}
	}
	a.failures = recent
	if len(a.failures) >= maxFailures {
		a.mu.Unlock()
		return ErrTooManyTries
	}
	a.mu.Unlock()

	got := sha256.Sum256([]byte(strings.TrimSpace(r.PostForm.Get("token"))))
	if subtle.ConstantTimeCompare(got[:], a.digest[:]) != 1 {
		a.mu.Lock()
		a.failures = append(a.failures, now)
		a.mu.Unlock()
		return ErrBadCredentials
	}
	a.Start(w, r)
	return nil
}

// Start begins a session for a request that has proved who it is by other means
// (a passkey): a random ID in a cookie that is HttpOnly, SameSite=Strict, without a
// Domain attribute (so only this host gets it) and Secure when the request came
// over HTTPS.
func (a *TokenAuth) Start(w http.ResponseWriter, r *http.Request) {
	id := randomHex(32)
	now := a.now()
	label, phone := deviceOf(r.UserAgent())
	idle := desktopIdle
	if phone {
		idle = phoneIdle
	}
	a.mu.Lock()
	a.sessions[sha256.Sum256([]byte(id))] = tokenSession{
		csrf: randomHex(32), expires: now.Add(sessionTTL), since: now, lastSeen: now, idle: idle, label: label, id: randomHex(6),
	}
	a.mu.Unlock()
	//nolint:gosec // Secure is set when the request came over HTTPS: the forwarder terminates TLS (D29) and the loopback listener is plain HTTP
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: isHTTPS(r), MaxAge: int(sessionTTL / time.Second),
	})
}

// SignOut implements Auth.
func (a *TokenAuth) SignOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		a.mu.Lock()
		delete(a.sessions, sha256.Sum256([]byte(c.Value)))
		a.mu.Unlock()
	}
	//nolint:gosec // clearing the cookie, with the same attributes it was set with
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r), MaxAge: -1})
}

// isHTTPS reports whether the browser reached the UI over HTTPS: directly, or
// through the forwarder (D29) that terminates TLS and says so.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// Devices implements the Devices interface: the live sessions, newest first, and
// the one of r marked.
func (a *TokenAuth) Devices(r *http.Request) []Device {
	var mine [sha256.Size]byte
	if c, err := r.Cookie(cookieName); err == nil {
		mine = sha256.Sum256([]byte(c.Value))
	}
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Device
	for key, s := range a.sessions {
		if !now.Before(s.expires) || now.Sub(s.lastSeen) > s.idle {
			delete(a.sessions, key)
			continue
		}
		out = append(out, Device{ID: s.id, Label: s.label, Since: s.since, LastSeen: s.lastSeen, Current: key == mine})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.After(out[j].Since) })
	return out
}

// Revoke implements the Devices interface.
func (a *TokenAuth) Revoke(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, s := range a.sessions {
		if s.id == id {
			delete(a.sessions, key)
			return true
		}
	}
	return false
}

// deviceOf names a browser from its User-Agent, in a few fixed words (the header
// is the client's, so none of it is kept), and says whether it is a phone.
func deviceOf(ua string) (label string, phone bool) {
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(ua, s) {
				return true
			}
		}
		return false
	}
	kind := "Computer"
	switch {
	case has("iPhone"), has("Android") && has("Mobile"):
		kind, phone = "Phone", true
	case has("iPad"), has("Android"):
		kind = "Tablet"
	case has("Macintosh"):
		kind = "Mac"
	case has("Windows"):
		kind = "Windows computer"
	case has("Linux"):
		kind = "Linux computer"
	}
	browser := "browser"
	switch {
	case has("Edg/"):
		browser = "Edge"
	case has("Firefox/", "FxiOS"):
		browser = "Firefox"
	case has("Chrome/", "CriOS"):
		browser = "Chrome"
	case has("Safari/"):
		browser = "Safari"
	}
	return kind + ", " + browser, phone
}
