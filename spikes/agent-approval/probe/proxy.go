package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// runProxy is a minimal logging allowlist proxy: HTTPS through CONNECT and plain
// HTTP with an absolute URI. Anything not on the allowlist gets a 403 and is
// logged. It is the spike's stand-in for the design's egress proxy (section 7.2).
func runProxy(listen, allow string) {
	allowed := map[string]bool{}
	for _, h := range strings.Split(allow, ",") {
		if h = strings.TrimSpace(h); h != "" {
			allowed[strings.ToLower(h)] = true
		}
	}
	permit := func(host string) bool {
		host = strings.ToLower(host)
		for h := range allowed {
			if host == h || strings.HasSuffix(host, "."+h) {
				return true
			}
		}
		return false
	}
	logf := func(verdict, method, host, from string) {
		fmt.Fprintf(os.Stderr, "%s %s %s %s from %s\n", time.Now().Format("15:04:05.000"), verdict, method, host, from)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Hostname()
		if r.Method == http.MethodConnect {
			host, _, _ = net.SplitHostPort(r.Host)
		}
		if !permit(host) {
			logf("DENY ", r.Method, host, r.RemoteAddr)
			http.Error(w, "blocked by egress policy", http.StatusForbidden)
			return
		}
		logf("ALLOW", r.Method, host, r.RemoteAddr)
		if r.Method == http.MethodConnect {
			dst, err := net.DialTimeout("tcp", r.Host, 8*time.Second)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			src, _, err := hj.Hijack()
			if err != nil {
				return
			}
			_, _ = src.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			go func() { defer dst.Close(); defer src.Close(); _, _ = io.Copy(dst, src) }()
			go func() { _, _ = io.Copy(src, dst) }()
			return
		}
		r.RequestURI = ""
		resp, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	fmt.Fprintln(os.Stderr, "egress proxy on", listen, "allowing", allow)
	srv := &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintln(os.Stderr, srv.ListenAndServe())
}
