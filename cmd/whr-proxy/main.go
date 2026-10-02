// Command whr-proxy is the egress allowlist proxy that runs in an
// environment's sidecar (design §7.2). It is built for the guest (linux-arm64)
// and mounted read-only into the sidecar.
//
//	whr-proxy -listen 0.0.0.0:3128 -allow api.anthropic.com,proxy.golang.org [-deny-prefixes 2001:db8:1::/64]
package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/egress"
)

// parsePrefixes reads the comma-separated -deny-prefixes list.
func parsePrefixes(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(list, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("-deny-prefixes: %w", err)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func main() {
	listen := flag.String("listen", "0.0.0.0:3128", "address to listen on")
	allow := flag.String("allow", "", "comma-separated host names that may be reached")
	deny := flag.String("deny-prefixes", "", "comma-separated IPv6 prefixes of the host that are refused like private ranges")
	flag.Parse()
	p := egress.New(strings.Split(*allow, ","))
	var err error
	if p.Deny, err = parsePrefixes(*deny); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	p.Log = func(line string) { fmt.Fprintln(os.Stderr, line) }
	fmt.Fprintln(os.Stderr, "egress proxy on", *listen, "allowing", *allow)
	// The read and write deadlines bound a plain HTTP exchange; a CONNECT
	// tunnel clears them and is bounded by the proxy's idle timeout instead.
	srv := &http.Server{
		Addr: *listen, Handler: p,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute,
		WriteTimeout: 10 * time.Minute, IdleTimeout: time.Minute,
	}
	fmt.Fprintln(os.Stderr, srv.ListenAndServe())
	os.Exit(1)
}
