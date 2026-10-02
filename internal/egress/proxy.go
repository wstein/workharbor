// Package egress is the logging allowlist proxy that runs in an environment's
// sidecar (design §7.2). The agent's network is internal, so its only way out is
// this proxy, which resolves names itself and lets through only the hosts it was
// given. Anything else, including a raw IP address, gets a 403 and is logged.
//
// Only ports 443 (CONNECT) and 80 (plain HTTP) are allowed, and a name that
// resolves to a loopback, private, link-local, CGNAT or other non-public
// address is refused: an allowed subdomain whose DNS an attacker controls must
// not reach the host or the LAN. The proxy dials the address it checked, so a
// DNS answer that changes between the check and the dial cannot slip through.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ErrNotPublic is returned for a destination whose address is not public.
var ErrNotPublic = errors.New("egress: the destination address is not public")

// DefaultIdleTimeout ends a tunnel in which nothing moved for this long.
const DefaultIdleTimeout = 5 * time.Minute

// Proxy is an allowlist HTTP and HTTPS (CONNECT) proxy.
type Proxy struct {
	allow []string
	// Log receives one line per decision: time, verdict, method, host and source.
	Log func(line string)
	// Dial connects to an allowed destination; by default the name is resolved,
	// every address must be public, and the checked address is dialled. Tests
	// replace it to reach a local server by an allowed name.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Transport carries plain HTTP requests; by default a transport that dials
	// like Dial and ignores any proxy settings of its own environment.
	Transport http.RoundTripper
	// Now is the clock of the log lines.
	Now func() time.Time
	// IdleTimeout closes a tunnel in which no byte moved in either direction
	// for this long; DefaultIdleTimeout when zero. A write that blocks counts
	// as no movement, so it also bounds a stuck writer.
	IdleTimeout time.Duration

	// lookup resolves a name; net.DefaultResolver by default.
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	// dialAddr dials a checked address literal; tests replace it.
	dialAddr  func(ctx context.Context, network, addr string) (net.Conn, error)
	transport http.RoundTripper
}

// New returns a proxy that allows the given host names and their subdomains.
func New(allow []string) *Proxy {
	p := &Proxy{}
	p.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           p.dialPublic,
		ResponseHeaderTimeout: time.Minute,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          16,
	}
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

// Public reports whether an address may be dialled: not loopback, private
// (RFC 1918, ULA), link-local, CGNAT, multicast, unspecified or otherwise
// reserved. An IPv4-mapped IPv6 address is judged as the IPv4 address.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range reserved {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// reserved are ranges netip does not flag but that are not the public internet.
// The NAT64 prefixes embed an IPv4 address that could be a private one.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, also Tailscale
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

// controlPublic is a net.Dialer.Control check: the socket's peer address must
// be public. It is the last line, after dialPublic's own check.
func controlPublic(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !Public(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrNotPublic, address)
	}
	return nil
}

// dialPublic resolves the host once, refuses it if any of its addresses is not
// public, and dials the checked address itself, so a second, different DNS
// answer is never used.
func (p *Proxy) dialPublic(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	portNum, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("egress: bad port %q", port)
	}
	lookup := p.lookup
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("egress: %s has no address", host)
	}
	for _, a := range addrs {
		if !Public(a) {
			return nil, fmt.Errorf("%w: %s resolves to %s", ErrNotPublic, host, a)
		}
	}
	dial := p.dialAddr
	if dial == nil {
		dial = (&net.Dialer{Timeout: 8 * time.Second, Control: controlPublic}).DialContext
	}
	var last error
	for _, a := range addrs {
		c, err := dial(ctx, network, netip.AddrPortFrom(a.Unmap(), uint16(portNum)).String())
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// allowedPort reports whether a method may reach a port: CONNECT only 443,
// plain HTTP only 80.
func allowedPort(method, port string) bool {
	if method == http.MethodConnect {
		return port == "443"
	}
	return port == "" || port == "80"
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
	} else if r.URL.Scheme != "http" {
		http.Error(w, "only plain http or CONNECT", http.StatusBadRequest)
		return
	}
	target := host
	if port != "" {
		target = net.JoinHostPort(host, port)
	}
	if !p.Allowed(host) || !allowedPort(r.Method, port) {
		p.logf("DENY ", r.Method, target, r.RemoteAddr)
		http.Error(w, "blocked by egress policy", http.StatusForbidden)
		return
	}
	p.logf("ALLOW", r.Method, target, r.RemoteAddr)
	if r.Method == http.MethodConnect {
		p.tunnel(w, r.RemoteAddr, host, port)
		return
	}
	p.forward(w, r)
}

func (p *Proxy) dialError(w http.ResponseWriter, method, target, from string, err error) {
	if errors.Is(err, ErrNotPublic) {
		p.logf("DENY ", method, target+" (not public)", from)
		http.Error(w, "blocked by egress policy", http.StatusForbidden)
		return
	}
	http.Error(w, "cannot reach the host", http.StatusBadGateway)
}

func (p *Proxy) tunnel(w http.ResponseWriter, from, host, port string) {
	dial := p.Dial
	if dial == nil {
		dial = p.dialPublic
	}
	target := net.JoinHostPort(host, port)
	dst, err := dial(context.Background(), "tcp", target)
	if err != nil {
		p.dialError(w, http.MethodConnect, target, from, err)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = dst.Close()
		http.Error(w, "cannot hijack", http.StatusInternalServerError)
		return
	}
	src, rw, err := hj.Hijack()
	if err != nil {
		_ = dst.Close()
		return
	}
	// The server's read and write deadlines may still be set on a hijacked
	// connection; the idle watchdog replaces them.
	_ = src.SetDeadline(time.Time{})
	_, _ = src.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	// Bytes the client sent in the same packet as the CONNECT (a TLS ClientHello
	// sent without waiting for the 200) are in the server's read buffer, not on the
	// connection: they go to the upstream first, or the tunnel stalls.
	if n := rw.Reader.Buffered(); n > 0 {
		if early, err := rw.Peek(n); err == nil {
			if _, err := dst.Write(early); err != nil {
				_ = dst.Close()
				_ = src.Close()
				return
			}
		}
	}
	p.pipe(src, dst)
}

// pipe copies both ways until both directions end or nothing moved for the
// idle timeout, then closes both connections.
func (p *Proxy) pipe(src, dst net.Conn) {
	idle := p.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	done := make(chan struct{})
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = dst.Close()
			_ = src.Close()
		})
	}
	go func() {
		tick := time.NewTicker(min(idle/4, time.Second))
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				if time.Since(time.Unix(0, last.Load())) >= idle {
					closeBoth()
					return
				}
			}
		}
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(a, b net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(activity{a, &last}, b)
		if c, ok := a.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}
	go cp(dst, src)
	go cp(src, dst)
	wg.Wait()
	close(done)
	closeBoth()
}

// activity records the time of every completed write.
type activity struct {
	w    io.Writer
	last *atomic.Int64
}

func (a activity) Write(b []byte) (int, error) {
	n, err := a.w.Write(b)
	a.last.Store(time.Now().UnixNano())
	return n, err
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) {
	rt := p.Transport
	if rt == nil {
		rt = p.transport
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Connection")
	resp, err := rt.RoundTrip(out)
	if err != nil {
		p.dialError(w, r.Method, r.URL.Host, r.RemoteAddr, err)
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
