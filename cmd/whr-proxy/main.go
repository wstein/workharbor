// Command whr-proxy is the egress allowlist proxy that runs in an
// environment's sidecar (design §7.2). It is built for the guest (linux-arm64)
// and mounted read-only into the sidecar.
//
//	whr-proxy -listen 0.0.0.0:3128 -allow api.anthropic.com,proxy.golang.org
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/egress"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:3128", "address to listen on")
	allow := flag.String("allow", "", "comma-separated host names that may be reached")
	flag.Parse()
	p := egress.New(strings.Split(*allow, ","))
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
