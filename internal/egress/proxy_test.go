package egress

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }

func TestAllowedByNameAndSubdomainNeverByIP(t *testing.T) {
	p := New([]string{"api.anthropic.com", "Example.COM", " ", "proxy.golang.org."})
	for host, want := range map[string]bool{
		"api.anthropic.com": true, "API.Anthropic.com": true, "example.com": true, "www.example.com": true, "a.b.example.com": true,
		"proxy.golang.org": true, "api.anthropic.com.": true,
		"github.com": false, "notexample.com": false, "example.com.evil.test": false, "anthropic.com": false,
		"": false, "1.1.1.1": false, "[2606:4700:4700::1111]": false, "2606:4700:4700::1111": false,
	} {
		if got := p.Allowed(host); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", host, got, want)
		}
	}
	// Even if an address is on the list, an IP is refused: the list is by name.
	if New([]string{"1.1.1.1"}).Allowed("1.1.1.1") {
		t.Error("a raw IP in the allowlist was allowed")
	}
}

func connect(t *testing.T, proxy, target string) (int, string) {
	t.Helper()
	c, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(c)
	line, _ := br.ReadString('\n')
	var code int
	_, _ = fmt.Sscanf(line, "HTTP/1.1 %d", &code)
	if code == 200 { // read the rest of the headers, then talk through the tunnel
		for {
			l, err := br.ReadString('\n')
			if err != nil || l == "\r\n" {
				break
			}
		}
		fmt.Fprintf(c, "ping\n")
		reply, _ := br.ReadString('\n')
		return code, strings.TrimSpace(reply)
	}
	return code, ""
}

func TestConnectAllowedDeniedAndRawIP(t *testing.T) {
	// A destination the proxy reaches for the allowed name.
	dest, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dest.Close() }()
	go func() {
		for {
			c, err := dest.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				line, _ := bufio.NewReader(c).ReadString('\n')
				fmt.Fprintf(c, "pong:%s", line)
			}()
		}
	}()
	var dialed []string
	var mu sync.Mutex
	var l logs
	p := New([]string{"allowed.example.test"})
	p.Log = l.add
	p.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, addr)
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, dest.Addr().String())
	}
	srv := httptest.NewServer(p)
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	if code, reply := connect(t, addr, "allowed.example.test:443"); code != 200 || reply != "pong:ping" {
		t.Errorf("allowed CONNECT: %d %q", code, reply)
	}
	if code, _ := connect(t, addr, "denied.example.test:443"); code != http.StatusForbidden {
		t.Errorf("denied CONNECT: %d, want 403", code)
	}
	if code, _ := connect(t, addr, "1.1.1.1:443"); code != http.StatusForbidden {
		t.Errorf("a raw IP CONNECT: %d, want 403", code)
	}
	if code, _ := connect(t, addr, "[2606:4700:4700::1111]:443"); code != http.StatusForbidden {
		t.Errorf("a raw IPv6 CONNECT: %d, want 403", code)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) != 1 || dialed[0] != "allowed.example.test:443" {
		t.Errorf("dialed %v: only the allowed name may be resolved and dialled", dialed)
	}
	// Every decision is logged with the verdict, the method and the host.
	joined := strings.Join(l.lines, "\n")
	for _, want := range []string{"ALLOW CONNECT allowed.example.test", "DENY  CONNECT denied.example.test", "DENY  CONNECT 1.1.1.1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log lacks %q:\n%s", want, joined)
		}
	}
}

func TestPlainHTTPThroughTheProxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "hello from origin")
	}))
	defer origin.Close()
	p := New([]string{"127.0.0.1"}) // an IP is never allowed, even when listed
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL+"/x", nil)
	req.RequestURI = ""
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("an absolute-URI request to an IP: %d, want 403", rec.Code)
	}

	// With an allowed name the request is forwarded through the transport.
	p2 := New([]string{"origin.test"})
	p2.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "origin.test" {
			t.Errorf("forwarded to %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Via": {"origin"}}, Body: http.NoBody}, nil
	})
	rec = httptest.NewRecorder()
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, "http://origin.test/path", nil)
	p2.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("X-Via") != "origin" {
		t.Errorf("allowed request: %d %v", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, "http://elsewhere.test/", nil)
	p2.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a denied host: %d", rec.Code)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
