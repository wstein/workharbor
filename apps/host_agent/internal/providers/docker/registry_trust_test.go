// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryEndpointRefusesPinnedToolPlaintextHeuristics(t *testing.T) {
	for _, host := range []string{"localhost", "localhost:5000", "a.localhost:443", "127.0.0.1:443", "10.1.2.3:443", "172.16.0.1:443", "192.168.1.2:443", "0.0.0.0:443", "http:", "https:"} {
		if registryTLS(host+"/fixture@sha256:"+strings.Repeat("a", 64)) == nil {
			t.Fatal("plaintext/ambiguous registry admitted", host)
		}
	}
	for _, ref := range []string{"ubuntu@sha256:", "docker.io/library/ubuntu@sha256:", "registry.workharbor.test:443/fixture@sha256:"} {
		if e := registryTLS(ref + strings.Repeat("a", 64)); e != nil {
			t.Fatal(ref, e)
		}
	}
}
func TestRegistryExplicitCABundleIntegrityAndPhysicalPath(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	root := bridgeRoot(t)
	path := filepath.Join(root, "fixture-ca.pem")
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	sum := sha256.Sum256(raw)
	os.WriteFile(path, raw, 0600)
	registry := Registry{CAFile: path, CASHA256: hex.EncodeToString(sum[:])}
	t.Setenv("WORKHARBOR_T009_REGISTRY_KEY_FILE", "PRIVATE_KEY_CANARY")
	environment, e := registry.readEnvironment(root, "/trusted/tool")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range environment {
		if strings.Contains(entry, "PRIVATE_KEY_CANARY") || strings.HasPrefix(entry, "WORKHARBOR_") {
			t.Fatal("fixture environment leaked to metadata tool")
		}
	}

	if _, e := registry.readEnvironment(root, "/trusted/tool"); e != nil {
		t.Fatal("valid explicit CA rejected", e)
	}
	for _, kind := range []string{"missing", "changed", "symlink", "wrong-hash", "non-ca"} {
		t.Run(kind, func(t *testing.T) {
			r := registry
			switch kind {
			case "missing":
				r.CAFile += ".missing"
			case "changed":
				r.CASHA256 = strings.Repeat("f", 64)
			case "symlink":
				link := filepath.Join(root, "ca-link")
				os.Symlink(path, link)
				r.CAFile = link
			case "wrong-hash":
				r.CASHA256 = ""
			case "non-ca":
				other := filepath.Join(root, "bad.pem")
				data := []byte("not a PEM certificate")
				os.WriteFile(other, data, 0600)
				hash := sha256.Sum256(data)
				r.CAFile = other
				r.CASHA256 = hex.EncodeToString(hash[:])
			}
			if _, e := r.readEnvironment(root, "/trusted/tool"); e == nil {
				t.Fatal("unverified CA admitted")
			}
		})
	}
}
