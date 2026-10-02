// Package preview is the preview proxy of design D33 (issue #72): it shows the
// developer a dev server the agent runs inside its environment, which sits on an
// internal network the developer's phone cannot reach.
//
// Each preview has a listener of its own on loopback, so the forwarder can give it
// its own HTTPS port and the browser its own origin, never the web UI's. It
// forwards to exactly one environment and one port, through the runtime's
// Previewer, whatever a request says. A single-use grant in the link becomes a
// per-preview cookie, and every other request needs that cookie. The preview ends
// when its environment stops, when it is closed, after MaxAge and when the
// supervisor restarts: nothing is stored.
//
// The app behind a preview is agent-written code and untrusted. A cookie is not
// kept apart by port, so the proxy never passes whr's own cookies to the app and
// never lets the app set one under their names.
package preview

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
)

// Limits of a preview.
const (
	// GrantTTL is how long the link's grant works, once.
	GrantTTL = 5 * time.Minute
	// DefaultMaxAge is how long a preview lives at most.
	DefaultMaxAge = 12 * time.Hour
	maxGrants     = 8
	liveFor       = 2 * time.Second
	// OwnCookiePrefix names every cookie whr sets, the UI's session included: none
	// of them reaches the app and the app sets none.
	OwnCookiePrefix = "whr_"
)

// Errors a caller can tell apart.
var (
	ErrUnavailable = errors.New("preview: this runtime cannot reach an environment's ports")
	ErrNotRunning  = errors.New("preview: the environment is not running")
	ErrNoPort      = errors.New("preview: no port is free in the preview range")
	ErrNotFound    = errors.New("preview: no such preview")
	ErrBadPort     = errors.New("preview: not a TCP port")
)

// Config configures a Manager.
type Config struct {
	// Upstream reaches an environment's port. Required.
	Upstream runtime.Previewer
	// FirstPort and LastPort are the loopback ports previews listen on, the ones
	// the forwarder maps. Zero means an ephemeral port, for tests.
	FirstPort, LastPort int
	// Scheme and Host make the link: the forwarder's name. Empty means
	// http://127.0.0.1. The listener's port is the link's port.
	Scheme, Host string
	// Live says whether an environment runs. Required: a stopped environment's
	// preview refuses.
	Live func(ctx context.Context, env string) bool
	// MaxAge bounds a preview; DefaultMaxAge when zero.
	MaxAge time.Duration
	Now    func() time.Time
	// OnClosed hears every preview that ended, and why.
	OnClosed func(p Preview, reason string)
	// OnError hears a problem that has no one to tell.
	OnError func(error)
}

// Preview describes an open preview. It carries no secret.
type Preview struct {
	ID      string    `json:"id"`
	Task    string    `json:"task"`
	Env     string    `json:"env"`
	Port    int       `json:"port"`   // the app's port in the environment
	Listen  int       `json:"listen"` // the port the preview listens on, the origin's
	Opened  time.Time `json:"opened"`
	Expires time.Time `json:"expires"`
}

// Manager holds the open previews.
type Manager struct {
	cfg Config

	mu       sync.Mutex
	previews map[string]*live
	seen     map[string]time.Time // env -> when it was last seen running
}

type live struct {
	Preview
	cookie string // the secret the cookie carries
	grants map[[32]byte]time.Time
	ln     net.Listener
	srv    *http.Server
	cancel context.CancelFunc
}

// New returns a Manager.
func New(cfg Config) (*Manager, error) {
	if cfg.Upstream == nil || cfg.Live == nil {
		return nil, errors.New("preview: an upstream and a liveness check are required")
	}
	if (cfg.FirstPort == 0) != (cfg.LastPort == 0) || cfg.LastPort < cfg.FirstPort {
		return nil, errors.New("preview: the port range is not valid")
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = DefaultMaxAge
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Manager{cfg: cfg, previews: map[string]*live{}, seen: map[string]time.Time{}}, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("preview: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Open opens a preview of one port of one environment for a task, or returns the
// one already open for that pair. The caller has checked that the port is one the
// environment declares and that the human may open it.
func (m *Manager) Open(ctx context.Context, task, env string, port int) (Preview, error) {
	if port < 1 || port > 65535 {
		return Preview{}, ErrBadPort
	}
	if !m.cfg.Live(ctx, env) {
		return Preview{}, ErrNotRunning
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.previews {
		if p.Env == env && p.Port == port {
			return p.Preview, nil
		}
	}
	ln, err := m.listen()
	if err != nil {
		return Preview{}, err
	}
	now := m.cfg.Now()
	p := &live{
		Preview: Preview{ID: "pv-" + randomHex(6), Task: task, Env: env, Port: port, Listen: ln.Addr().(*net.TCPAddr).Port, Opened: now, Expires: now.Add(m.cfg.MaxAge)},
		cookie:  randomHex(32), grants: map[[32]byte]time.Time{}, ln: ln,
	}
	ctx2, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.srv = &http.Server{
		Handler:           m.handler(p),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx2 },
	}
	m.previews[p.ID] = p
	m.seen[env] = now
	go func() {
		if err := p.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && m.cfg.OnError != nil {
			m.cfg.OnError(fmt.Errorf("preview %s: %w", p.ID, err))
		}
	}()
	return p.Preview, nil
}

// listen takes the first free port of the range, or an ephemeral one.
func (m *Manager) listen() (net.Listener, error) {
	var lc net.ListenConfig
	if m.cfg.FirstPort == 0 {
		return lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	}
	used := map[int]bool{}
	for _, p := range m.previews {
		used[p.Listen] = true
	}
	for port := m.cfg.FirstPort; port <= m.cfg.LastPort; port++ {
		if used[port] {
			continue
		}
		if ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
			return ln, nil
		}
	}
	return nil, ErrNoPort
}

// Grant makes the link that opens a preview in a browser: a single-use grant,
// valid for GrantTTL. It is what the human is sent to; the app never sees it.
func (m *Manager) Grant(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.previews[id]
	if !ok {
		return "", ErrNotFound
	}
	now := m.cfg.Now()
	for h, exp := range p.grants {
		if !now.Before(exp) {
			delete(p.grants, h)
		}
	}
	if len(p.grants) >= maxGrants {
		return "", errors.New("preview: too many open links: use one of them or wait a few minutes")
	}
	g := randomHex(16)
	p.grants[sha256.Sum256([]byte(g))] = now.Add(GrantTTL)
	return m.base(p) + "/?" + grantParam + "=" + g, nil
}

const grantParam = "whr_preview"

func (m *Manager) base(p *live) string {
	scheme, host := m.cfg.Scheme, m.cfg.Host
	if host == "" {
		scheme, host = "http", "127.0.0.1"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(p.Listen))
}

// List returns the open previews, oldest first.
func (m *Manager) List() []Preview {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Preview, 0, len(m.previews))
	for _, p := range m.previews {
		out = append(out, p.Preview)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Opened.Equal(out[j].Opened) {
			return out[i].Opened.Before(out[j].Opened)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Close ends a preview: its listener, its connections (a WebSocket included) and
// its token. Closing one that is gone is not an error.
func (m *Manager) Close(id, reason string) {
	m.mu.Lock()
	p, ok := m.previews[id]
	delete(m.previews, id)
	m.mu.Unlock()
	if !ok {
		return
	}
	p.cancel()
	_ = p.srv.Close()
	if m.cfg.OnClosed != nil {
		m.cfg.OnClosed(p.Preview, reason)
	}
}

// CloseEnv ends every preview of an environment.
func (m *Manager) CloseEnv(env, reason string) {
	for _, p := range m.List() {
		if p.Env == env {
			m.Close(p.ID, reason)
		}
	}
}

// Sweep closes the previews whose environment stopped and those past MaxAge.
func (m *Manager) Sweep(ctx context.Context) {
	now := m.cfg.Now()
	for _, p := range m.List() {
		switch {
		case !now.Before(p.Expires):
			m.Close(p.ID, "expired")
		case !m.running(ctx, p.Env):
			m.Close(p.ID, "environment stopped")
		}
	}
}

// Run sweeps every few seconds until ctx ends, then closes every preview.
func (m *Manager) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 10 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, p := range m.List() {
				m.Close(p.ID, "supervisor stopped")
			}
			return
		case <-t.C:
			m.Sweep(ctx)
		}
	}
}

// running asks whether an environment runs, at most every liveFor.
func (m *Manager) running(ctx context.Context, env string) bool {
	m.mu.Lock()
	last, ok := m.seen[env]
	m.mu.Unlock()
	if ok && m.cfg.Now().Sub(last) < liveFor {
		return true
	}
	if !m.cfg.Live(ctx, env) {
		m.mu.Lock()
		delete(m.seen, env)
		m.mu.Unlock()
		return false
	}
	m.mu.Lock()
	m.seen[env] = m.cfg.Now()
	m.mu.Unlock()
	return true
}

func ownCookie(name string) bool {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "__Host-"), "__Secure-")
	return strings.HasPrefix(name, OwnCookiePrefix)
}

func (m *Manager) alive(id string) (*live, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.previews[id]
	return p, ok
}

// hostOK refuses a request that was not made to the preview's own name: the
// forwarder's, or loopback. The port is not compared, because the forwarder may
// map another. It is what stops a name that resolves to this listener (DNS
// rebinding) from reaching the app.
func (m *Manager) hostOK(r *http.Request) bool {
	h := r.Host
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.Trim(strings.ToLower(h), "[]")
	switch h {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return m.cfg.Host != "" && h == strings.ToLower(m.cfg.Host)
}

func (m *Manager) handler(p *live) http.Handler {
	cookieName := OwnCookiePrefix + "preview_" + strings.TrimPrefix(p.ID, "pv-")
	target := "localhost:" + strconv.Itoa(p.Port)
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Only the path and query of the request are used: the host, the scheme
			// and any port in an absolute URI are replaced, so there is nothing for
			// a request to steer.
			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.Host = "http", target, target
			pr.SetXForwarded()
			pr.Out.Header.Del("Cookie")
			for _, c := range pr.In.Cookies() {
				if !ownCookie(c.Name) {
					pr.Out.AddCookie(c)
				}
			}
		},
		Transport: &http.Transport{
			// The address is ignored: the connection goes to this preview's
			// environment and port and to nothing else.
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return m.cfg.Upstream.DialPreview(ctx, p.Env, p.Port)
			},
			MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, DisableCompression: true,
		},
		ModifyResponse: func(resp *http.Response) error {
			kept := resp.Header.Values("Set-Cookie")[:0:0]
			for _, v := range resp.Header.Values("Set-Cookie") {
				name, _, _ := strings.Cut(v, "=")
				if !ownCookie(strings.TrimSpace(name)) {
					kept = append(kept, v)
				}
			}
			resp.Header.Del("Set-Cookie")
			for _, v := range kept {
				resp.Header.Add("Set-Cookie", v)
			}
			if resp.Header.Get("Referrer-Policy") == "" {
				resp.Header.Set("Referrer-Policy", "no-referrer")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if m.cfg.OnError != nil && !errors.Is(err, context.Canceled) {
				m.cfg.OnError(fmt.Errorf("preview %s: %w", p.ID, err))
			}
			http.Error(w, "The app did not answer. Is its dev server running?", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.hostOK(r) {
			http.Error(w, "wrong host", http.StatusMisdirectedRequest)
			return
		}
		if _, ok := m.alive(p.ID); !ok {
			http.Error(w, "this preview is closed", http.StatusGone)
			return
		}
		if !m.cfg.Now().Before(p.Expires) {
			http.Error(w, "this preview is closed", http.StatusGone)
			m.closeAfter(w, p.ID, "expired")
			return
		}
		if !m.running(r.Context(), p.Env) {
			http.Error(w, "the environment is stopped", http.StatusServiceUnavailable)
			m.closeAfter(w, p.ID, "environment stopped")
			return
		}
		if g := r.URL.Query().Get(grantParam); g != "" {
			m.exchange(w, r, p, cookieName, g)
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(p.cookie)) != 1 {
			http.Error(w, "Open this preview from workharbor.", http.StatusUnauthorized)
			return
		}
		rp.ServeHTTP(w, r)
	})
}

// closeAfter closes a preview once the answer in w has been sent: closing it
// inside its own handler would cut that answer off.
func (m *Manager) closeAfter(w http.ResponseWriter, id, reason string) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go m.Close(id, reason)
}

// exchange trades a grant for the preview's cookie and removes the grant from
// the address.
func (m *Manager) exchange(w http.ResponseWriter, r *http.Request, p *live, cookieName, grant string) {
	h := sha256.Sum256([]byte(grant))
	m.mu.Lock()
	exp, ok := p.grants[h]
	delete(p.grants, h) // single use, valid or not
	m.mu.Unlock()
	if !ok || !m.cfg.Now().Before(exp) {
		http.Error(w, "This link was used or has expired. Open the preview again from workharbor.", http.StatusForbidden)
		return
	}
	//nolint:gosec // Secure is set when the link is https: the forwarder terminates TLS (D29) and the loopback listener is plain HTTP
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: p.cookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: m.cfg.Scheme == "https",
	})
	q := r.URL.Query()
	q.Del(grantParam)
	next := url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
	if next.Path == "" {
		next.Path = "/"
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, next.String(), http.StatusSeeOther)
}
