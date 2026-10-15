// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Pinned crane chooses plaintext for these literal registry authorities even
// without --insecure. Refuse them before any metadata process or provider effect.
// DNS names use the tools' HTTPS entrypoint; digest validation remains mandatory.
func registryTLS(ref string) error {
	name, _, ok := registryRef(ref)
	if !ok {
		return fail("unsupported_configuration")
	}
	first := strings.SplitN(name, "/", 2)[0]
	if !strings.Contains(name, "/") || !strings.ContainsAny(first, ".:") && first != "localhost" {
		return nil
	}
	host := first
	if strings.Contains(first, ":") {
		var port string
		var e error
		host, port, e = net.SplitHostPort(first)
		if e != nil {
			return fail("unsupported_configuration")
		}
		number, e := strconv.Atoi(port)
		if e != nil || number < 1 || number > 65535 {
			return fail("unsupported_configuration")
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fail("unsupported_configuration")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return fail("unsupported_configuration")
	}
	return nil
}
func (r Registry) readEnvironment(root, tool string) ([]string, error) {
	env := registryEnvironment(root, tool)
	if r.CAFile == "" {
		if r.CASHA256 != "" {
			return nil, fail("tool_unavailable")
		}
		return env, nil
	}
	if !filepath.IsAbs(r.CAFile) || filepath.Clean(r.CAFile) != r.CAFile || !hexID.MatchString(r.CASHA256) {
		return nil, fail("tool_unavailable")
	}
	physical, e := filepath.EvalSymlinks(r.CAFile)
	info, se := os.Lstat(r.CAFile)
	if e != nil || se != nil || physical != r.CAFile || !ownedRegular(info) || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > metadataLimit {
		return nil, fail("tool_unavailable")
	}
	f, e := os.Open(r.CAFile)
	if e != nil {
		return nil, fail("tool_unavailable")
	}
	actual, fe := f.Stat()
	raw, re := io.ReadAll(io.LimitReader(f, metadataLimit+1))
	ce := f.Close()
	current, le := os.Lstat(r.CAFile)
	if fe != nil || re != nil || ce != nil || le != nil || !os.SameFile(info, actual) || !os.SameFile(info, current) || !ownedRegular(current) || len(raw) > metadataLimit {
		return nil, fail("tool_unavailable")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != r.CASHA256 {
		return nil, fail("tool_unavailable")
	}
	rest := raw
	count := 0
	for len(strings.TrimSpace(string(rest))) > 0 {
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, fail("tool_unavailable")
		}
		certificate, e := x509.ParseCertificate(block.Bytes)
		if e != nil || !certificate.IsCA || time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
			return nil, fail("tool_unavailable")
		}
		count++
		rest = tail
	}
	if count == 0 {
		return nil, fail("tool_unavailable")
	}
	return append(env, "SSL_CERT_FILE="+r.CAFile), nil
}
