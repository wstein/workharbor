package egress

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublicRefusesNonPublicAddresses(t *testing.T) {
	for addr, want := range map[string]bool{
		"1.1.1.1": true, "203.0.113.7": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.0.0.1": false, "172.16.0.1": false, "192.168.64.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "100.100.100.100": false, "0.0.0.0": false,
		"0.1.2.3": false, "224.0.0.1": false, "255.255.255.255": false, "198.18.0.1": false,
		"::1": false, "::": false, "fe80::1": false, "fd00::1": false, "fc00::1": false, "ff02::1": false,
		"::ffff:10.0.0.1": false, "::ffff:127.0.0.1": false, "::ffff:1.1.1.1": true, "64:ff9b::a00:1": false,
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
	if err := controlPublic("tcp4", "192.168.64.1:443", nil); !errors.Is(err, ErrNotPublic) {
		t.Errorf("control for a private address = %v", err)
	}
	if err := controlPublic("tcp6", "[2606:4700:4700::1111]:443", nil); err != nil {
		t.Errorf("control for a public address = %v", err)
	}
}

func TestOnlyPorts443And80(t *testing.T) {
	var l logs
	p := New([]string{"allowed.example.test"})
	p.Log = l.add
	p.Dial = func(context.Context, string, string) (net.Conn, error) {
		t.Error("a refused port was dialled")
		return nil, errors.New("no")
	}
	srv := httptest.NewServer(p)
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	for _, target := range []string{"allowed.example.test:22", "allowed.example.test:80", "allowed.example.test:8443"} {
		if code, _ := connect(t, addr, target); code != http.StatusForbidden {
			t.Errorf("CONNECT %s: %d, want 403", target, code)
		}
	}
	p.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("a refused port was forwarded")
		return nil, errors.New("no")
	})
	for _, u := range []string{"http://allowed.example.test:6379/", "http://allowed.example.test:443/"} {
		rec := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s: %d, want 403", u, rec.Code)
		}
	}
	if joined := strings.Join(l.lines, "\n"); !strings.Contains(joined, "DENY  CONNECT allowed.example.test:22") {
		t.Errorf("the log lacks the port:\n%s", joined)
	}
}

// fakeDNS answers each lookup from its list in turn.
type fakeDNS struct {
	mu      sync.Mutex
	answers [][]string
	dialled []string
}

func (f *fakeDNS) lookup(_ context.Context, _ string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []netip.Addr
	for _, a := range f.answers[0] {
		out = append(out, netip.MustParseAddr(a))
	}
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return out, nil
}

func (f *fakeDNS) dial(_ context.Context, _, addr string) (net.Conn, error) {
	f.mu.Lock()
	f.dialled = append(f.dialled, addr)
	f.mu.Unlock()
	a, b := net.Pipe()
	go func() { _ = b.Close() }()
	return a, nil
}

func TestASubdomainResolvingToAPrivateAddressIsRefused(t *testing.T) {
	for name, answer := range map[string][]string{
		"private":       {"192.168.64.1"},
		"loopback v6":   {"::1"},
		"link-local v6": {"fe80::1"},
		"mapped":        {"::ffff:10.0.0.1"},
		"cgnat":         {"100.64.0.1"},
		"mixed":         {"1.1.1.1", "10.0.0.1"},
	} {
		dns := &fakeDNS{answers: [][]string{answer}}
		var l logs
		p := New([]string{"evil.example.test"})
		p.Log = l.add
		p.lookup, p.dialAddr = dns.lookup, dns.dial
		srv := httptest.NewServer(p)
		if code, _ := connect(t, strings.TrimPrefix(srv.URL, "http://"), "evil.example.test:443"); code != http.StatusForbidden {
			t.Errorf("%s: CONNECT = %d, want 403", name, code)
		}
		rec := httptest.NewRecorder()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://evil.example.test/", nil)
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: GET = %d, want 403", name, rec.Code)
		}
		srv.Close()
		if len(dns.dialled) != 0 {
			t.Errorf("%s: dialled %v", name, dns.dialled)
		}
		if !strings.Contains(strings.Join(l.lines, "\n"), "(not public)") {
			t.Errorf("%s: the refusal was not logged: %v", name, l.lines)
		}
	}
}

// DNS rebinding: the first answer is public, the next private. Each connection
// dials the address literal it checked, so the private answer is never used.
func TestDNSRebindingCannotSlipBetweenCheckAndDial(t *testing.T) {
	dns := &fakeDNS{answers: [][]string{{"203.0.113.7"}, {"192.168.64.1"}}}
	p := New([]string{"rebind.example.test"})
	p.lookup, p.dialAddr = dns.lookup, dns.dial
	ctx := context.Background()
	c, err := p.dialPublic(ctx, "tcp", "rebind.example.test:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if _, err := p.dialPublic(ctx, "tcp", "rebind.example.test:443"); !errors.Is(err, ErrNotPublic) {
		t.Errorf("the rebound answer = %v, want ErrNotPublic", err)
	}
	if len(dns.dialled) != 1 || dns.dialled[0] != "203.0.113.7:443" {
		t.Errorf("dialled %v, want only the checked literal 203.0.113.7:443", dns.dialled)
	}
	// The default dialer's own control check refuses a private literal too.
	q := New(nil)
	q.lookup = (&fakeDNS{answers: [][]string{{"127.0.0.1"}}}).lookup
	if _, err := q.dialPublic(ctx, "tcp", "x.test:443"); !errors.Is(err, ErrNotPublic) {
		t.Errorf("loopback = %v", err)
	}
}

func TestAnIdleTunnelIsClosed(t *testing.T) {
	dest, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dest.Close() }()
	go func() {
		c, err := dest.Accept()
		if err == nil {
			defer func() { _ = c.Close() }()
			_, _ = c.Read(make([]byte, 1)) // never answers
		}
	}()
	p := New([]string{"slow.example.test"})
	p.IdleTimeout = 200 * time.Millisecond
	p.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, dest.Addr().String())
	}
	srv := httptest.NewServer(p)
	defer srv.Close()
	c, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write([]byte("CONNECT slow.example.test:443 HTTP/1.1\r\nHost: slow.example.test:443\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 512)
	n, _ := c.Read(buf)
	if !strings.Contains(string(buf[:n]), "200") {
		t.Fatalf("CONNECT reply %q", buf[:n])
	}
	start := time.Now()
	if _, err := c.Read(buf); err == nil {
		t.Fatal("the idle tunnel sent data")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() { //nolint:errorlint // a direct net.Error
		t.Fatal("the idle tunnel was not closed by the proxy")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("closed after %v", d)
	}
}
