// Command fakegithub is a stand-in for the GitHub API for the integration run:
// it answers the calls the GitHub App client makes (installation, token, one
// issue, the repository) on a loopback port and logs each request without any
// credential. It checks nothing about the JWT: the client's own tests do.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:8798", "loopback address")
	repo := flag.String("repo", "wstein/workharbor", "the repository")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Fatalf("fakegithub: %q is not a loopback address", *addr)
	}
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s\n", r.Method, r.URL.Path) // never the Authorization header
		switch r.URL.Path {
		case "/repos/" + *repo + "/installation":
			reply(w, 200, map[string]int{"id": 7})
		case "/app/installations/7/access_tokens":
			reply(w, 201, map[string]any{"token": "ghs_fake" + strings.Repeat("0", 32), "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		case "/repos/" + *repo + "/issues/1":
			reply(w, 200, map[string]any{
				"number": 1, "title": "Add a greeting to the README", "author_association": "OWNER",
				"body": "Add one line to README.md that says hello. This is an integration test issue.",
				"user": map[string]string{"login": "wstein"},
			})
		case "/repos/" + *repo:
			reply(w, 200, map[string]string{"default_branch": "main"})
		default:
			reply(w, 404, map[string]string{"message": "Not Found"})
		}
	})
	log.Fatal((&http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}
