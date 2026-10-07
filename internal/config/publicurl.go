package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var hostLabelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizePublicURL turns what a person typed for whr's public name (D29)
// into the https origin the configuration holds: "WHR.Example.ts.net/" becomes
// "https://whr.example.ts.net". A missing scheme becomes https, never http. An
// explicit http:// is refused (the forwarder terminates TLS; a passkey and a
// Secure cookie need https), as is any other scheme, userinfo, path, query,
// fragment, backslash, space or a host that is not a DNS name or an IP address.
// The result is what the GitHub App manifest redirects the browser to, so it
// must not be steerable to another site.
func NormalizePublicURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("the public name is empty")
	}
	if strings.ContainsAny(s, " \t\r\n\\@?#") {
		return "", errors.New("the public name is a host name, optionally with https:// and a port: no spaces, user, path, query or fragment")
	}
	rest := s
	if i := strings.Index(s, "://"); i >= 0 {
		scheme := strings.ToLower(s[:i])
		if scheme == "http" {
			return "", errors.New("http:// is refused: whr's public name is https (the forwarder terminates TLS)")
		}
		if scheme != "https" {
			return "", fmt.Errorf("the scheme %q is not https", scheme)
		}
		rest = s[i+3:]
	}
	rest = strings.TrimSuffix(rest, "/")
	if strings.Contains(rest, "/") {
		return "", errors.New("the public name has no path: give the host name only")
	}
	u, err := url.Parse("https://" + rest)
	if err != nil || u.Host == "" {
		return "", errors.New("that is not a host name")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if ip == nil {
		if len(host) > 253 {
			return "", errors.New("the host name is too long")
		}
		for _, l := range strings.Split(host, ".") {
			if !hostLabelRE.MatchString(l) {
				return "", fmt.Errorf("%q is not a host name", host)
			}
		}
	}
	if p := u.Port(); p != "" {
		if n := atoiPort(p); n < 1 {
			return "", fmt.Errorf("the port %q is not 1 to 65535", p)
		}
		return "https://" + net.JoinHostPort(host, p), nil
	}
	if ip != nil && strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "https://" + host, nil
}

func atoiPort(p string) int {
	n := 0
	for _, r := range p {
		if r < '0' || r > '9' || n > 65535 {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	if n > 65535 {
		return 0
	}
	return n
}
