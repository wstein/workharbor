// Package egress is the logging allowlist proxy that runs in an environment's
// sidecar (design §7.2). The agent's network is internal, so its only way out is
// this proxy, which resolves names itself and lets through only the hosts it was
// given. Anything else, including a raw IP address, gets a 403 and is logged.
package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Proxy is an allowlist HTTP and HTTPS (CONNECT) proxy.
type Proxy struct {
	allow []string
	// Log receives one line per decision: time, verdict, method, host and source.
	Log func(line string)
	// Dial connects to an allowed destination; net.Dialer by default. Tests
	// replace it to reach a local server by an allowed name.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Transport carries plain HTTP requests; http.DefaultTransport by default.
	Transport http.RoundTripper
	// Now is the clock of the log lines.
	Now func() time.Time
}

// New returns a proxy that allows the given host names and their subdomains.
func New(allow []string) *Proxy {
	p := &Proxy{}
	for _, h := range allow {
		if h = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(h, "."))); h != "" {
			p.allow = append(p.allow, h)
		}
	}
	return p
}

// Allowed reports whether a host may be reached. A host matches an entry when
// it is that name or a subdomain of it. An IP address never matches, whatever
// the list says: the allowlist is by name, and the name is what the proxy
// resolves.
func (p *Proxy) Allowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || net.ParseIP(strings.Trim(host, "[]")) != nil {
		return false
	}
	for _, a := range p.allow {
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}

func (p *Proxy) logf(verdict, method, host, from string) {
	if p.Log == nil {
		return
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	p.Log(fmt.Sprintf("%s %s %s %s from %s", now().UTC().Format("15:04:05.000"), verdict, method, host, from))
}

// ServeHTTP implements http.Handler: CONNECT for HTTPS, an absolute URI for
// plain HTTP.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Hostname()
	port := r.URL.Port()
	if r.Method == http.MethodConnect {
		var err error
		host, port, err = net.SplitHostPort(r.Host)
		if err != nil {
			http.Error(w, "bad CONNECT target", http.StatusBadRequest)
			return
		}
	}
	if !p.Allowed(host) {
		p.logf("DENY ", r.Method, host, r.RemoteAddr)
		http.Error(w, "blocked by egress policy", http.StatusForbidden)
		return
	}
	p.logf("ALLOW", r.Method, host, r.RemoteAddr)
	if r.Method == http.MethodConnect {
		p.tunnel(w, host, port)
		return
	}
	p.forward(w, r)
}

func (p *Proxy) tunnel(w http.ResponseWriter, host, port string) {
	dial := p.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 8 * time.Second}).DialContext
	}
	dst, err := dial(context.Background(), "tcp", net.JoinHostPort(host, port))
	if err != nil {
		http.Error(w, "cannot reach the host", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = dst.Close()
		http.Error(w, "cannot hijack", http.StatusInternalServerError)
		return
	}
	src, _, err := hj.Hijack()
	if err != nil {
		_ = dst.Close()
		return
	}
	_, _ = src.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(a, b net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		if c, ok := a.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}
	go pipe(dst, src)
	go pipe(src, dst)
	wg.Wait()
	_ = dst.Close()
	_ = src.Close()
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) {
	rt := p.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Connection")
	resp, err := rt.RoundTrip(out)
	if err != nil {
		http.Error(w, "cannot reach the host", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
