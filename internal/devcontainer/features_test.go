package devcontainer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/devcontainer/feature"
	"github.com/wstein/workharbor/internal/oci"
)

// featureRegistry serves features by repository name: each a manifest and a blob.
type featureRegistry struct {
	blobs map[string][]byte // repo -> archive
	srv   *httptest.Server
}

func newFeatureRegistry(t *testing.T, features map[string]map[string]string) *featureRegistry {
	t.Helper()
	r := &featureRegistry{blobs: map[string][]byte{}}
	for repo, files := range features {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for name, body := range files {
			_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))})
			_, _ = tw.Write([]byte(body))
		}
		_ = tw.Close()
		r.blobs[repo] = buf.Bytes()
	}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		for repo, blob := range r.blobs {
			if !strings.Contains(q.URL.Path, "/"+repo+"/") {
				continue
			}
			sum := sha256.Sum256(blob)
			layer := oci.Descriptor{MediaType: oci.MediaFeatureLayer, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(blob))}
			if strings.Contains(q.URL.Path, "/manifests/") {
				m, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": oci.MediaManifest, "layers": []oci.Descriptor{layer}})
				_, _ = w.Write(m)
				return
			}
			_, _ = w.Write(blob)
			return
		}
		http.NotFound(w, q)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *featureRegistry) opts() Options {
	o := testOpts
	o.Features = &feature.Resolver{Client: oci.New(oci.Config{HTTP: &http.Client{}, BaseURL: func(string) string { return r.srv.URL }})}
	return o
}

func resolveWith(t *testing.T, o Options, files map[string]string) (Environment, error) {
	t.Helper()
	g := &fakeGit{files: map[string]string{}, modes: map[string]string{}}
	for k, v := range files {
		g.files[strings.Repeat("a", 40)+":"+k] = v
	}
	return Resolve(context.Background(), g, "main", o)
}

const nodeFeature = `{"id":"node","version":"1.7.1","options":{"version":{"type":"string","default":"lts"}},
"containerEnv":{"NVM_DIR":"/usr/local/share/nvm","PATH":"/usr/local/share/nvm/current/bin:${PATH}"},
"installsAfter":["ghcr.io/devcontainers/features/common-utils"]}`

const plainFeature = `{"id":"x","version":"1.0.0"}`

func TestFeaturesAreFetchedOrderedAndBuiltOnTheImage(t *testing.T) {
	reg := newFeatureRegistry(t, map[string]map[string]string{
		"devcontainers/features/node":         {"devcontainer-feature.json": nodeFeature, "install.sh": "#!/bin/sh\n"},
		"devcontainers/features/common-utils": {"devcontainer-feature.json": plainFeature, "install.sh": "#!/bin/sh\n"},
	})
	env, err := resolveWith(t, reg.opts(), map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{
			"ghcr.io/devcontainers/features/node:1":{"version":"22"},
			"ghcr.io/devcontainers/features/common-utils:2":{}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !env.Built() || len(env.Features) != 2 {
		t.Fatalf("built = %v, features = %d", env.Built(), len(env.Features))
	}
	// node asks to install after common-utils, so common-utils goes first
	if !strings.Contains(env.Features[0].Ref, "common-utils") || !strings.Contains(env.Features[1].Ref, "node") {
		t.Errorf("order = %s, %s", env.Features[0].Ref, env.Features[1].Ref)
	}
	if !strings.HasPrefix(env.Features[1].Digest, "sha256:") {
		t.Errorf("the digest was not recorded: %q", env.Features[1].Digest)
	}
	// the tag follows the digests and the options: another version changes it
	tag := env.Tag("whr")
	again, err := resolveWith(t, reg.opts(), map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{
			"ghcr.io/devcontainers/features/node:1":{"version":"22"},
			"ghcr.io/devcontainers/features/common-utils:2":{}}}`,
	})
	if err != nil || again.Tag("whr") != tag {
		t.Errorf("the same features gave another tag: %v %s %s", err, again.Tag("whr"), tag)
	}
	other, _ := resolveWith(t, reg.opts(), map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{
			"ghcr.io/devcontainers/features/node:1":{"version":"20"},
			"ghcr.io/devcontainers/features/common-utils:2":{}}}`,
	})
	if other.Tag("whr") == tag {
		t.Error("another option kept the tag")
	}

	// staged: a context with the features and a Dockerfile that installs them in order
	dir := t.TempDir()
	b, _, err := env.Stage(context.Background(), nil, "whr", dir)
	if err != nil {
		t.Fatal(err)
	}
	df, err := os.ReadFile(b.Dockerfile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FROM docker.io/library/ubuntu:24.04\n", "/tmp/whr-features/0 &&", "/tmp/whr-features/1 &&", `ENV PATH="/usr/local/share/nvm/current/bin:${PATH}"`} {
		if !strings.Contains(string(df), want) {
			t.Errorf("the Dockerfile lacks %q:\n%s", want, df)
		}
	}
	if opt, err := os.ReadFile(filepath.Join(b.ContextDir, "features", "1", "whr-options.env")); err != nil || !strings.Contains(string(opt), "export VERSION='22'") { //nolint:gosec // a test path
		t.Errorf("node's options file = %q, %v", opt, err)
	}
	if b.Tag != tag {
		t.Errorf("the build tag %s is not the environment's %s", b.Tag, tag)
	}
}

// A refused feature or a foreign source is left out with a note and the environment
// still resolves; a foreign source is recorded to be asked about.
func TestAnUnsafeOrForeignFeatureIsLeftOutWithANote(t *testing.T) {
	reg := newFeatureRegistry(t, map[string]map[string]string{
		"devcontainers/features/node":       {"devcontainer-feature.json": plainFeature, "install.sh": "x"},
		"devcontainers/features/privileged": {"devcontainer-feature.json": `{"id":"p","privileged":true}`, "install.sh": "x"},
		"someone/else/thing":                {"devcontainer-feature.json": plainFeature, "install.sh": "x"},
	})
	env, err := resolveWith(t, reg.opts(), map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{
			"ghcr.io/devcontainers/features/node:1":{},
			"ghcr.io/devcontainers/features/privileged:1":{},
			"ghcr.io/someone/else/thing:1":{}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Features) != 1 || !strings.Contains(env.Features[0].Ref, "node") {
		t.Fatalf("features applied = %+v", env.Features)
	}
	notes := strings.Join(env.Notes, "\n")
	for _, want := range []string{"privileged:1 is not applied", "outside ghcr.io/devcontainers/features/", "privileged"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	// the foreign one is recorded to be asked about, as written
	if len(env.ForeignFeatures) != 1 || env.ForeignFeatures[0] != "ghcr.io/someone/else/thing:1" {
		t.Errorf("foreign features = %v", env.ForeignFeatures)
	}
	// without a resolver they stay requested too, and the tag is the plain one
	plain, err := resolveWith(t, testOpts, map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{"ghcr.io/devcontainers/features/node:1":{}}}`,
	})
	if err != nil || plain.Built() || !strings.Contains(strings.Join(plain.Notes, "|"), "no feature resolver") {
		t.Errorf("without a resolver: built %v, notes %v, %v", plain.Built(), plain.Notes, err)
	}
}

// A registry that cannot be reached, or serves what does not check, fails the
// resolution instead of leaving an environment without its tools.
func TestAFeatureThatCannotBeFetchedFailsTheResolution(t *testing.T) {
	reg := newFeatureRegistry(t, map[string]map[string]string{})
	_, err := resolveWith(t, reg.opts(), map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{"ghcr.io/devcontainers/features/node:1":{}}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "devcontainer feature ghcr.io/devcontainers/features/node:1") {
		t.Errorf("an unreachable feature: %v", err)
	}
	// a cycle between features is refused
	cyc := newFeatureRegistry(t, map[string]map[string]string{
		"devcontainers/features/a": {"devcontainer-feature.json": `{"id":"a","installsAfter":["ghcr.io/devcontainers/features/b"]}`, "install.sh": "x"},
		"devcontainers/features/b": {"devcontainer-feature.json": `{"id":"b","installsAfter":["ghcr.io/devcontainers/features/a"]}`, "install.sh": "x"},
	})
	if _, err := resolveWith(t, cyc.opts(), map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{"ghcr.io/devcontainers/features/a:1":{},"ghcr.io/devcontainers/features/b:1":{}}}`,
	}); err == nil || !strings.Contains(err.Error(), "wait on each other") {
		t.Errorf("a cycle: %v", err)
	}
}

func TestFeatureOptionsInTheFileAreReadAsStrings(t *testing.T) {
	c, err := Parse([]byte(`{"image":"i","features":{
		"a:1":"22", "b:1":true, "c:1":false, "d:1":{"n":3,"on":true,"s":"x"}, "e:1":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]string{}
	var order []string
	for _, r := range c.FeatureRequests {
		got[r.ID] = r.Options
		order = append(order, r.ID)
	}
	if strings.Join(order, ",") != "a:1,b:1,d:1,e:1" { // c is switched off
		t.Errorf("order = %v", order)
	}
	if got["a:1"]["version"] != "22" || got["d:1"]["n"] != "3" || got["d:1"]["on"] != "true" || got["d:1"]["s"] != "x" {
		t.Errorf("options = %v", got)
	}
	for _, bad := range []string{`{"image":"i","features":{"a:1":{"x":[1]}}}`, `{"image":"i","features":{"a:1":5}}`, `{"image":"i","features":["a:1"]}`, `{"image":"i","features":{"a:1":{},"a:1":{}}}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

// A foreign source the human allowed for the repository is applied like any other.
func TestAnApprovedForeignFeatureIsApplied(t *testing.T) {
	reg := newFeatureRegistry(t, map[string]map[string]string{
		"someone/else/thing": {"devcontainer-feature.json": plainFeature, "install.sh": "x"},
	})
	o := reg.opts()
	r := *o.Features
	r.Approved = func(ref string) bool { return ref == "ghcr.io/someone/else/thing:1" }
	o.Features = &r
	env, err := resolveWith(t, o, map[string]string{
		".devcontainer.json": `{"image":"docker.io/library/ubuntu:24.04","features":{"ghcr.io/someone/else/thing:1":{},"ghcr.io/other/one:2":{}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Features) != 1 || len(env.ForeignFeatures) != 1 || env.ForeignFeatures[0] != "ghcr.io/other/one:2" {
		t.Errorf("features %d, foreign %v", len(env.Features), env.ForeignFeatures)
	}
}

// Features of a Dockerfile environment apply on top of the image the Dockerfile builds:
// two builds, the second from the tag of the first.
func TestFeaturesApplyOnTopOfADockerfileImage(t *testing.T) {
	reg := newFeatureRegistry(t, map[string]map[string]string{
		"devcontainers/features/node": {"devcontainer-feature.json": plainFeature, "install.sh": "x"},
	})
	g := &fakeGit{files: map[string]string{}, modes: map[string]string{}}
	g.files[strings.Repeat("a", 40)+":.devcontainer/devcontainer.json"] = `{"build":{"dockerfile":"Dockerfile"},"features":{"ghcr.io/devcontainers/features/node:1":{}}}`
	g.files[strings.Repeat("a", 40)+":.devcontainer/Dockerfile"] = "FROM scratch\n"
	env, err := Resolve(context.Background(), g, "main", reg.opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Features) != 1 || !env.FeaturesOnDockerfile() || !env.Built() {
		t.Fatalf("features %d, on dockerfile %v", len(env.Features), env.FeaturesOnDockerfile())
	}
	base, final := env.BaseTag("whr"), env.Tag("whr")
	if base == final {
		t.Fatal("the image with features has the tag of the one without")
	}
	first, _, err := env.Stage(context.Background(), g, "whr", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if first.Tag != base {
		t.Errorf("the Dockerfile build is tagged %s, want %s", first.Tag, base)
	}
	second, _, err := env.StageFeatures("whr", t.TempDir(), base)
	if err != nil {
		t.Fatal(err)
	}
	df, _ := os.ReadFile(second.Dockerfile)
	if second.Tag != final || !strings.HasPrefix(string(df), "FROM "+base+"\n") {
		t.Errorf("second build %s from:\n%s", second.Tag, df)
	}
}
