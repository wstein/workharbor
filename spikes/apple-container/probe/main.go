// Command probe is a tiny static helper for the isolation tests. It runs on the
// host (listeners) and inside throwaway containers (connect tests), so the guest
// image needs no extra tools.
//
//	probe listen-tcp  <addr>        accept TCP and answer "pong"
//	probe listen-unix <path>        accept on a unix socket and answer "pong"
//	probe dial-tcp    <addr>        try to connect, print ok or the error
//	probe dial-unix   <path>        try to connect to a unix socket
//	probe http-get    <url> [proxy] GET a URL, optionally through an HTTP proxy
//	probe proxy       <addr> <allowed,hosts> logging allowlist HTTP/CONNECT proxy
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

func serve(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			fmt.Fprintf(os.Stderr, "accepted from %s\n", c.RemoteAddr())
			_, _ = c.Write([]byte("pong\n"))
		}()
	}
}

func dial(network, addr string) {
	c, err := net.DialTimeout(network, addr, 3*time.Second)
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b, _ := io.ReadAll(c)
	fmt.Printf("ok: %q\n", string(b))
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: probe <mode> <arg> [arg2]")
		os.Exit(2)
	}
	mode, arg := os.Args[1], os.Args[2]
	switch mode {
	case "listen-tcp":
		l, err := net.Listen("tcp", arg)
		if err != nil {
			fmt.Println("FAIL:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "listening on", l.Addr())
		serve(l)
	case "listen-unix":
		_ = os.Remove(arg)
		l, err := net.Listen("unix", arg)
		if err != nil {
			fmt.Println("FAIL:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "listening on", arg)
		serve(l)
	case "dial-tcp":
		dial("tcp", arg)
	case "dial-unix":
		dial("unix", arg)
	case "proxy":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: probe proxy <addr> <allowed,hosts>")
			os.Exit(2)
		}
		runProxy(arg, os.Args[3])
	case "http-get":
		tr := &http.Transport{}
		if len(os.Args) > 3 {
			p, err := url.Parse(os.Args[3])
			if err != nil {
				fmt.Println("FAIL:", err)
				os.Exit(1)
			}
			tr.Proxy = http.ProxyURL(p)
		}
		cl := &http.Client{Transport: tr, Timeout: 8 * time.Second}
		resp, err := cl.Get(arg)
		if err != nil {
			fmt.Println("FAIL:", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		fmt.Println("status:", resp.Status)
	default:
		fmt.Fprintln(os.Stderr, "unknown mode", mode)
		os.Exit(2)
	}
}
