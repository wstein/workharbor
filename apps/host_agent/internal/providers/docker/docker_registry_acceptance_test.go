//go:build docker_integration

// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/storage"
)

func rawRegistryRefusals(t *testing.T, ctx context.Context, host Provider, binding storage.Binding, mutate func(map[string]any) config.Descriptor, original map[string]any, root, ledger string) {
	t.Helper()
	caPath, keyPath := realEnv(t, "WORKHARBOR_T009_REGISTRY_CA_FILE"), realEnv(t, "WORKHARBOR_T009_REGISTRY_KEY_FILE")
	caHash, keyHash := os.Getenv("WORKHARBOR_T009_REGISTRY_CA_SHA256"), os.Getenv("WORKHARBOR_T009_REGISTRY_KEY_SHA256")
	if !hexID.MatchString(caHash) || !hexID.MatchString(keyHash) || realHash(t, caPath) != caHash || realHash(t, keyPath) != keyHash {
		t.Fatal("unbound real fixture TLS assets")
	}
	for _, path := range []string{caPath, keyPath} {
		actual, e := filepath.EvalSymlinks(path)
		info, se := os.Lstat(path)
		if e != nil || se != nil || actual != path || !ownedRegular(info) || info.Mode().Perm() != 0600 {
			t.Fatal("unsafe fixture TLS asset", path)
		}
	}
	certificateBytes, e := os.ReadFile(caPath)
	if e != nil {
		t.Fatal(e)
	}
	block, _ := pem.Decode(certificateBytes)
	if block == nil {
		t.Fatal("fixture CA malformed")
	}
	certificate, e := x509.ParseCertificate(block.Bytes)
	if e != nil || certificate.VerifyHostname("registry.workharbor.test") != nil || !certificate.IsCA {
		t.Fatal("wrong registry fixture SAN/CA", e)
	}
	certificatePair, e := tls.LoadX509KeyPair(caPath, keyPath)
	if e != nil {
		t.Fatal(e)
	}
	addresses, e := net.LookupIP("registry.workharbor.test")
	if e != nil || len(addresses) == 0 {
		t.Fatal("missing reviewed guest-only registry hosts mapping", e)
	}
	for _, ip := range addresses {
		if !ip.Equal(net.IPv4(127, 0, 0, 1)) {
			t.Fatal("registry DNS leaves owned loopback fixture")
		}
	}
	port, e := openNativePort(ctx, nativeOptions{Policy: DriverPolicy{Phase: Preflight, Scope: scopeFixture()}, Scope: scopeFixture(), Check: func(ctx context.Context) error { return ctx.Err() }, DockerPath: host.DockerPath, Socket: host.Socket, BuildxPath: host.BuildxPath, PrivateDirectory: host.PrivateDirectory})
	if e != nil {
		t.Fatal(e)
	}
	defer port.Close()
	snapshot := func() string {
		var result strings.Builder
		for _, args := range [][]string{{"ps", "-aq", "--no-trunc"}, {"image", "ls", "-q", "--no-trunc"}, {"volume", "ls", "-q"}} {
			raw, e := port.read(ctx, args)
			if e != nil {
				t.Fatal(e)
			}
			result.Write(raw)
			result.WriteByte('\n')
		}
		return result.String()
	}
	baseline := snapshot()
	proof := map[string]any{}
	for _, kind := range []string{"duplicate-raw-key", "onbuild", "volumes", "host-metadata", "malformed-metadata", "unknown-raw-field"} {
		t.Run("raw-"+kind, func(t *testing.T) {
			forwarded := fixtureLedger(t, ledger)
			cfg := map[string]any{"architecture": runtimeArchitecture(), "os": "linux", "rootfs": map[string]any{"type": "layers", "diff_ids": []any{}}, "config": map[string]any{"User": "0:0", "Labels": map[string]any{}}}
			properties := cfg["config"].(map[string]any)
			switch kind {
			case "onbuild":
				properties["OnBuild"] = []any{"RUN touch /workspace/RAW_MUST_NOT_RUN"}
			case "volumes":
				properties["Volumes"] = map[string]any{"/workspace/app": map[string]any{}}
			case "host-metadata":
				properties["Labels"] = map[string]any{"devcontainer.metadata": `[{"initializeCommand":"touch ` + filepath.Join(root, "RAW_HOST_MUST_NOT_RUN") + `"}]`}
			case "malformed-metadata":
				properties["Labels"] = map[string]any{"devcontainer.metadata": "["}
			case "unknown-raw-field":
				properties["WorkHarborUnknown"] = true
			}
			configBytes, _ := json.Marshal(cfg)
			if kind == "duplicate-raw-key" {
				configBytes = bytes.Replace(configBytes, []byte(`"User":"0:0"`), []byte(`"User":"0:0","User":"0:0"`), 1)
			}
			configSum := sha256.Sum256(configBytes)
			configDigest := "sha256:" + hex.EncodeToString(configSum[:])
			manifest := map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest, "size": len(configBytes)}, "layers": []any{}}
			manifestBytes, _ := json.Marshal(manifest)
			manifestSum := sha256.Sum256(manifestBytes)
			manifestDigest := "sha256:" + hex.EncodeToString(manifestSum[:])
			var mu sync.Mutex
			requests := map[string]int{}
			unexpected := []string{}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests[r.Method+" "+r.URL.Path]++
				mu.Unlock()
				if r.TLS == nil || r.Method != "GET" && r.Method != "HEAD" || r.URL.RawQuery != "" {
					mu.Lock()
					unexpected = append(unexpected, r.Method+" "+r.URL.Path)
					mu.Unlock()
					http.Error(w, "refused", 400)
					return
				}
				var raw []byte
				digest, contentType := "", "application/json"
				switch r.URL.Path {
				case "/v2/":
					raw = []byte("{}")
				case "/v2/fixture/manifests/" + manifestDigest:
					raw = manifestBytes
					digest = manifestDigest
					contentType = "application/vnd.oci.image.manifest.v1+json"
				case "/v2/fixture/blobs/" + configDigest:
					raw = configBytes
					digest = configDigest
				default:
					mu.Lock()
					unexpected = append(unexpected, r.Method+" "+r.URL.Path)
					mu.Unlock()
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
				w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
				if digest != "" {
					w.Header().Set("Docker-Content-Digest", digest)
				}
				if r.Method == "GET" {
					w.Write(raw)
				}
			})
			server := httptest.NewUnstartedServer(handler)
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificatePair}}
			server.StartTLS()
			defer server.Close()
			_, number, e := net.SplitHostPort(server.Listener.Addr().String())
			if e != nil {
				t.Fatal(e)
			}
			reference := "registry.workharbor.test:" + number + "/fixture@" + manifestDigest
			source := clone(original).(map[string]any)
			delete(source, "build")
			source["image"] = reference
			descriptor := mutate(source)
			caseHost := host
			caseHost.Registry.CAFile = caPath
			caseHost.Registry.CASHA256 = caHash
			caseBinding := binding
			caseBinding.Initial = descriptor
			prepared, e := caseHost.Prepare(ctx, realAdmission(descriptor, caseBinding, 1))
			if e == nil {
				prepared.Close()
				t.Fatal("hostile raw metadata admitted")
			}
			mu.Lock()
			blobReads := requests["GET /v2/fixture/blobs/"+configDigest]
			bad := append([]string{}, unexpected...)
			record := map[string]int{}
			for k, v := range requests {
				record[k] = v
			}
			mu.Unlock()
			if blobReads == 0 {
				t.Fatalf("raw config not reached: error=%v requests=%v", e, record)
			}
			if len(bad) != 0 {
				t.Fatal("non-metadata/plaintext/layer request", bad)
			}
			if !bytes.Equal(forwarded, fixtureLedger(t, ledger)) {
				t.Fatal("raw refusal forwarded mutating Docker command")
			}
			if snapshot() != baseline {
				t.Fatal("raw refusal changed daemon inventory")
			}
			if _, e := os.Lstat(filepath.Join(host.Store.Root, binding.StorageID)); !os.IsNotExist(e) {
				t.Fatal("raw refusal published data", e)
			}
			if _, e := os.Lstat(filepath.Join(root, "RAW_HOST_MUST_NOT_RUN")); !os.IsNotExist(e) {
				t.Fatal("raw metadata host hook executed", e)
			}
			proof[kind] = map[string]any{"raw_config_sha256": hex.EncodeToString(configSum[:]), "manifest_digest": manifestDigest, "config_size": len(configBytes), "metadata_requests": record, "daemon_unchanged": true, "zero_mutating_forwards": true, "seed_absent": true, "hooks_absent": true}
		})
	}
	raw, _ := json.MarshalIndent(map[string]any{"ca_sha256": caHash, "server_name": "registry.workharbor.test", "cases": proof}, "", "  ")
	realWrite(t, filepath.Join(root, "raw-registry-proof.json"), append(raw, '\n'))
}
func runtimeArchitecture() string { return strings.TrimSpace(runtime.GOARCH) }
