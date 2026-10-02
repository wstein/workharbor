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
	"strings"
	"sync"
	"time"
)

// Session is who a request is, as far as a handler needs to know. CSRF is the
// token every POST of the session must carry.
type Session struct {
	CSRF string
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

// Errors of signing in.
var (
	ErrBadCredentials = errors.New("that is not the token")
	ErrTooManyTries   = errors.New("too many failed tries: wait a minute")
)

const (
	cookieName    = "whr_session"
	sessionTTL    = 12 * time.Hour
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
	csrf    string
	expires time.Time
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
	if !ok || !a.now().Before(s.expires) {
		delete(a.sessions, key)
		return Session{}, false
	}
	return Session{CSRF: s.csrf}, true
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
	id := randomHex(32)
	a.mu.Lock()
	a.sessions[sha256.Sum256([]byte(id))] = tokenSession{csrf: randomHex(32), expires: now.Add(sessionTTL)}
	a.mu.Unlock()
	//nolint:gosec // Secure is set when the request came over HTTPS: the forwarder terminates TLS (D29) and the loopback listener is plain HTTP
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: isHTTPS(r), MaxAge: int(sessionTTL / time.Second),
	})
	return nil
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
