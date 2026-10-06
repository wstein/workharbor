package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var bg = context.Background()

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

type tarEntry struct {
	name, body, link string
	typ              byte
	mode             int64
}

func makeTar(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: e.mode, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	return buf.Bytes()
}

// registry is a fake: a token endpoint, a manifest and a blob served from another host,
// as ghcr.io redirects to its storage.
type registry struct {
	api, store *httptest.Server
	blob       []byte
	manifest   []byte
	headerDig  string // Docker-Content-Digest to send, if set
	tokenRealm string
	gotAuth    map[string]string           // path -> Authorization seen
	blobBody   func(w http.ResponseWriter) // overrides the blob answer
}

func newRegistry(t *testing.T, blob []byte) *registry {
	t.Helper()
	r := &registry{blob: blob, gotAuth: map[string]string{}}
	r.store = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.gotAuth["store"] = q.Header.Get("Authorization")
		if r.blobBody != nil {
			r.blobBody(w)
			return
		}
		_, _ = w.Write(r.blob)
	}))
	t.Cleanup(r.store.Close)
	layer := Descriptor{MediaType: MediaFeatureLayer, Digest: digestOf(blob), Size: int64(len(blob))}
	r.manifest, _ = json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": MediaManifest, "layers": []Descriptor{layer}})
	r.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.gotAuth[q.URL.Path] = q.Header.Get("Authorization")
		switch {
		case q.URL.Path == "/token":
			realmOK := q.URL.Query().Get("scope") == "repository:devcontainers/features/node:pull"
			if !realmOK {
				http.Error(w, "bad scope", 400)
				return
			}
			_, _ = w.Write([]byte(`{"token":"anon"}`))
			return
		case q.Header.Get("Authorization") != "Bearer anon":
			realm := r.tokenRealm
			if realm == "" {
				realm = r.api.URL + "/token"
			}
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm=%q,service="fake"`, realm))
			w.WriteHeader(401)
		case strings.Contains(q.URL.Path, "/manifests/"):
			if r.headerDig != "" {
				w.Header().Set("Docker-Content-Digest", r.headerDig)
			}
			w.Header().Set("Content-Type", MediaManifest)
			_, _ = w.Write(r.manifest)
		case strings.Contains(q.URL.Path, "/blobs/"):
			http.Redirect(w, q, r.store.URL+"/blob", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, q)
		}
	}))
	t.Cleanup(r.api.Close)
	return r
}

func (r *registry) client(mod ...func(*Config)) *Client {
	cfg := Config{HTTP: &http.Client{Timeout: 5 * time.Second}, BaseURL: func(string) string { return r.api.URL }}
	for _, m := range mod {
		m(&cfg)
	}
	return New(cfg)
}

func ref(t *testing.T) Ref {
	t.Helper()
	r, err := ParseRef("ghcr.io/devcontainers/features/node:1")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseRef(t *testing.T) {
	good := map[string]Ref{
		"ghcr.io/devcontainers/features/node:1":           {Registry: "ghcr.io", Repo: "devcontainers/features/node", Tag: "1"},
		"GHCR.io/a/b:1.2.3":                               {Registry: "ghcr.io", Repo: "a/b", Tag: "1.2.3"},
		"localhost:5000/a:v1":                             {Registry: "localhost:5000", Repo: "a", Tag: "v1"},
		"ghcr.io/a/b@sha256:" + strings.Repeat("a", 64):   {Registry: "ghcr.io", Repo: "a/b", Digest: "sha256:" + strings.Repeat("a", 64)},
		"ghcr.io/a/b:1@sha256:" + strings.Repeat("b", 64): {Registry: "ghcr.io", Repo: "a/b", Tag: "1", Digest: "sha256:" + strings.Repeat("b", 64)},
		// A tag may hold dots anywhere after its first character (OCI distribution spec); it is one
		// path segment, never a traversal. FuzzParseRef found "0/0:0..".
		"0/0:0..": {Registry: "0", Repo: "0", Tag: "0.."},
	}
	for s, want := range good {
		if got, err := ParseRef(s); err != nil || got != want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", s, got, err, want)
		}
	}
	for _, s := range []string{"", "node", "ghcr.io/a/b", "ghcr.io/a/b:", "ghcr.io/A_/b:1", "ghcr.io/../b:1", "ghcr.io/a b/c:1", "-x/a:1", "ghcr.io/a/b:1@sha256:zz", "ghcr.io/a//b:1", "ghcr.io/a/b:1?x", "ghcr.io/a/b:/x", "https://ghcr.io/a:1", "ghcr.io\\a/b:1", "a..b/x:1", ".a/x:1", "a./x:1"} {
		if _, err := ParseRef(s); !errors.Is(err, ErrBadRef) {
			t.Errorf("ParseRef(%q) = %v, want ErrBadRef", s, err)
		}
	}
}

// The manifest is pinned by the hash of its bytes, the blob is followed to its other
// host without the token, and everything checks.
func TestResolveAndFetchAFeature(t *testing.T) {
	blob := makeTar(t, tarEntry{name: "install.sh", body: "#!/bin/sh\n", mode: 0o755}, tarEntry{name: "devcontainer-feature.json", body: "{}", mode: 0o644})
	reg := newRegistry(t, blob)
	c := reg.client()
	m, err := c.Resolve(bg, ref(t))
	if err != nil {
		t.Fatal(err)
	}
	if m.Digest != digestOf(reg.manifest) || len(m.Layers) != 1 || m.Layers[0].MediaType != MediaFeatureLayer {
		t.Fatalf("manifest = %+v", m)
	}
	var out bytes.Buffer
	if err := c.Blob(bg, ref(t), m.Layers[0], &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), blob) {
		t.Error("the blob is not what was served")
	}
	if reg.gotAuth["store"] != "" {
		t.Errorf("the token went to the storage host: %q", reg.gotAuth["store"])
	}
	// pinned by digest: the right one resolves, a wrong one is refused
	pinned := ref(t)
	pinned.Digest = m.Digest
	if _, err := c.Resolve(bg, pinned); err != nil {
		t.Errorf("a pinned manifest: %v", err)
	}
	pinned.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := c.Resolve(bg, pinned); !errors.Is(err, ErrDigest) {
		t.Errorf("a manifest that is not the pinned one: %v", err)
	}
}

func TestAWrongOrMovedBlobOrManifestIsRefused(t *testing.T) {
	blob := []byte("the feature")
	// the registry's own header disagrees with the manifest's bytes
	reg := newRegistry(t, blob)
	reg.headerDig = "sha256:" + strings.Repeat("1", 64)
	if _, err := reg.client().Resolve(bg, ref(t)); !errors.Is(err, ErrDigest) {
		t.Errorf("a Docker-Content-Digest that does not match: %v", err)
	}
	// a blob that hashes to something else
	reg = newRegistry(t, blob)
	reg.blobBody = func(w http.ResponseWriter) { _, _ = w.Write([]byte("another one")) }
	m, err := reg.client().Resolve(bg, ref(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.client().Blob(bg, ref(t), m.Layers[0], &bytes.Buffer{}); !errors.Is(err, ErrDigest) && !errors.Is(err, ErrTooLarge) {
		t.Errorf("a blob with the wrong content: %v", err)
	}
	// a blob longer than declared is cut off and refused
	reg.blobBody = func(w http.ResponseWriter) {
		_, _ = w.Write(append([]byte("the feature"), bytes.Repeat([]byte("x"), 1000)...))
	}
	var sink bytes.Buffer
	if err := reg.client().Blob(bg, ref(t), m.Layers[0], &sink); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a blob longer than declared: %v", err)
	}
	if sink.Len() > len(blob)+1 {
		t.Errorf("%d bytes were written for a %d byte blob", sink.Len(), len(blob))
	}
	// a layer bigger than the cap is not even fetched
	big := Descriptor{MediaType: MediaFeatureLayer, Digest: digestOf(blob), Size: 1 << 30}
	if err := reg.client().Blob(bg, ref(t), big, &bytes.Buffer{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a layer over the cap: %v", err)
	}
}

func TestOnlyAnOCIImageManifestWithLayersIsAFeature(t *testing.T) {
	for name, manifest := range map[string]string{
		"an index":        `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`,
		"no layers":       `{"schemaVersion":2,"mediaType":"` + MediaManifest + `","layers":[]}`,
		"schema 1":        `{"schemaVersion":1,"layers":[{"digest":"sha256:` + strings.Repeat("a", 64) + `","size":1}]}`,
		"a bad digest":    `{"schemaVersion":2,"layers":[{"digest":"md5:abc","size":1}]}`,
		"a negative size": `{"schemaVersion":2,"layers":[{"digest":"sha256:` + strings.Repeat("a", 64) + `","size":-1}]}`,
		"not json":        `<html>`,
	} {
		reg := newRegistry(t, []byte("x"))
		reg.manifest = []byte(manifest)
		if _, err := reg.client().Resolve(bg, ref(t)); !errors.Is(err, ErrManifest) {
			t.Errorf("%s: %v, want ErrManifest", name, err)
		}
	}
	reg := newRegistry(t, []byte("x"))
	reg.manifest = bytes.Repeat([]byte(" "), 4096)
	if _, err := reg.client(func(c *Config) { c.MaxManifest = 1024 }).Resolve(bg, ref(t)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a manifest over the cap: %v", err)
	}
}

// A registry cannot send the client to a token endpoint on another host.
func TestTheTokenRealmMustBeOnTheRegistryItself(t *testing.T) {
	reg := newRegistry(t, []byte("x"))
	reg.tokenRealm = "https://evil.example/token"
	if _, err := reg.client().Resolve(bg, ref(t)); err == nil || !strings.Contains(err.Error(), "is not on ghcr.io") {
		t.Errorf("a foreign realm: %v", err)
	}
	reg.tokenRealm = "http://" + strings.TrimPrefix(reg.api.URL, "http://") + "/token@evil.example"
	if _, err := reg.client().Resolve(bg, ref(t)); err == nil {
		t.Error("a realm with userinfo was followed")
	}
}

// The default client dials only public addresses: a registry (or a redirect) that
// names the host's own network gets nowhere.
func TestTheDefaultClientRefusesAPrivateAddress(t *testing.T) {
	reg := newRegistry(t, []byte("x"))
	c := New(Config{BaseURL: func(string) string { return reg.api.URL }}) // 127.0.0.1
	if _, err := c.Resolve(bg, ref(t)); err == nil || !strings.Contains(err.Error(), "not public") {
		t.Errorf("a loopback registry: %v", err)
	}
}

// --- extraction ---

func TestExtractWritesFilesAndDirectoriesOnly(t *testing.T) {
	dir := t.TempDir()
	data := makeTar(t,
		tarEntry{name: "./", typ: tar.TypeDir, mode: 0o755},
		tarEntry{name: "install.sh", body: "echo hi\n", mode: 0o750},
		tarEntry{name: "lib/", typ: tar.TypeDir, mode: 0o700},
		tarEntry{name: "lib/data.txt", body: "d", mode: 0o666},
	)
	if err := Extract(bytes.NewReader(data), dir, ExtractLimits{}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"install.sh": 0o755, "lib/data.txt": 0o644} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s = %v, %v; want %v", name, fi, err, want)
		}
	}
	// gzip is read too
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write(data)
	_ = w.Close()
	if err := Extract(&gz, t.TempDir(), ExtractLimits{}); err != nil {
		t.Errorf("a gzip archive: %v", err)
	}
}

func TestExtractRefusesWhatCouldEscapeOrLinkOut(t *testing.T) {
	for name, entries := range map[string][]tarEntry{
		"an absolute path":    {{name: "/etc/passwd", body: "x"}},
		"a parent element":    {{name: "../evil", body: "x"}},
		"a nested parent":     {{name: "a/../../evil", body: "x"}},
		"a backslash":         {{name: `a\..\evil`, body: "x"}},
		"a symbolic link":     {{name: "l", typ: tar.TypeSymlink, link: "/etc"}},
		"a hard link":         {{name: "h", typ: tar.TypeLink, link: "install.sh"}},
		"a device":            {{name: "dev", typ: tar.TypeChar}},
		"a fifo":              {{name: "pipe", typ: tar.TypeFifo}},
		"a duplicate":         {{name: "a", body: "1"}, {name: "a", body: "2"}},
		"a file below a link": {{name: "l", typ: tar.TypeSymlink, link: "../.."}, {name: "l/x", body: "x"}},
		"too deep":            {{name: strings.Repeat("d/", 20) + "f", body: "x"}},
	} {
		dir := t.TempDir()
		err := Extract(bytes.NewReader(makeTar(t, entries...)), dir, ExtractLimits{})
		if !errors.Is(err, ErrUnsafeArchive) {
			t.Errorf("%s: %v, want ErrUnsafeArchive", name, err)
		}
		// nothing was written outside the directory
		if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
			t.Errorf("%s: a file escaped", name)
		}
	}
	// limits
	many := make([]tarEntry, 0, 11)
	for i := range 11 {
		many = append(many, tarEntry{name: fmt.Sprintf("f%d", i), body: "x"})
	}
	if err := Extract(bytes.NewReader(makeTar(t, many...)), t.TempDir(), ExtractLimits{Files: 10, Bytes: 1 << 20, Depth: 4}); !errors.Is(err, ErrUnsafeArchive) {
		t.Errorf("too many files: %v", err)
	}
	// directories count against the same limit: a bomb of empty directories is refused too
	dirs := make([]tarEntry, 0, 11)
	for i := range 11 {
		dirs = append(dirs, tarEntry{name: fmt.Sprintf("d%d", i), typ: tar.TypeDir})
	}
	if err := Extract(bytes.NewReader(makeTar(t, dirs...)), t.TempDir(), ExtractLimits{Files: 10, Bytes: 1 << 20, Depth: 4}); !errors.Is(err, ErrUnsafeArchive) {
		t.Errorf("too many directories: %v", err)
	}
	if err := Extract(bytes.NewReader(makeTar(t, tarEntry{name: "big", body: strings.Repeat("x", 100)})), t.TempDir(), ExtractLimits{Files: 10, Bytes: 50, Depth: 4}); !errors.Is(err, ErrUnsafeArchive) {
		t.Errorf("too many bytes: %v", err)
	}
	if err := Extract(strings.NewReader("not a tar at all"), t.TempDir(), ExtractLimits{}); !errors.Is(err, ErrUnsafeArchive) {
		t.Errorf("garbage: %v", err)
	}
}

func makeTarForSeed(name, body string, mode int64) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(body))})
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	return buf.Bytes()
}
