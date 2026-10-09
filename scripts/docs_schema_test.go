package scripts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var docsSite = flag.String("docs-site", "", "already built documentation site")

var (
	siteOnce sync.Once
	siteDir  string
	siteErr  error
)

// TestMain removes the site this package built itself; a site given with
// -docs-site belongs to the caller and stays.
func TestMain(m *testing.M) {
	code := m.Run()
	if siteDir != "" {
		_ = os.RemoveAll(siteDir)
	}
	os.Exit(code)
}

// builtSite returns the already built site of -docs-site, else builds it once
// for all tests of this package.
func builtSite(t *testing.T) string {
	t.Helper()
	if *docsSite != "" {
		return *docsSite
	}
	siteOnce.Do(func() {
		dir, err := os.MkdirTemp("", "docs-site-")
		if err != nil {
			siteErr = err
			return
		}
		siteDir = dir
		cmd := exec.CommandContext(context.Background(), "make", "docs-build", "DOCS_DEST="+dir) //nolint:gosec // destination is created by MkdirTemp
		cmd.Dir = ".."
		// Keep the tool caches and HOME; isolate git invoked by make or Hugo.
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if !strings.HasPrefix(key, "GIT_") && key != "SSH_AUTH_SOCK" {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_TERMINAL_PROMPT=0", "SSH_AUTH_SOCK=", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o IdentityAgent=none")
		if out, err := cmd.CombinedOutput(); err != nil {
			siteErr = fmt.Errorf("build documentation: %w\n%s", err, out)
		}
	})
	if siteErr != nil {
		t.Fatal(siteErr)
	}
	return siteDir
}

// TestDocsSchema checks the actual build artifact, not a second tracked schema.
func TestDocsSchema(t *testing.T) {
	site := builtSite(t)
	source, err := os.ReadFile("../internal/api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join(site, "openapi.json")) //nolint:gosec // test build artifact
	if err != nil {
		t.Fatalf("published schema missing: %v", err)
	}
	if !bytes.Equal(source, published) {
		t.Fatal("published openapi.json differs from internal/api/openapi.json")
	}
	var contract struct {
		Paths map[string]map[string]struct {
			Summary string `json:"summary"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(source, &contract); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(site, "docs", "manual", "api-reference", "index.html")) //nolint:gosec // test build artifact
	if err != nil {
		t.Fatal(err)
	}
	rendered := html.UnescapeString(string(page))
	for path, operations := range contract.Paths {
		if !strings.Contains(rendered, path) {
			t.Errorf("reference missing route %s", path)
		}
		for method, operation := range operations {
			if operation.Summary != "" && !strings.Contains(rendered, operation.Summary) {
				t.Errorf("reference missing %s %s summary", method, path)
			}
		}
	}
	for name := range contract.Components.Schemas {
		if !bytes.Contains(page, []byte("schema-"+name)) {
			t.Errorf("reference missing schema %s", name)
		}
	}
}

// TestDocsPublishesTheJSONSchemas checks that every file of schemas/ reaches
// the site unchanged at /schemas/<name> (issue #268), and that each name
// carries its surface and a version, so a historical file keeps its URL.
func TestDocsPublishesTheJSONSchemas(t *testing.T) {
	site := builtSite(t)
	names, err := filepath.Glob("../schemas/*")
	if err != nil || len(names) == 0 {
		t.Fatalf("no schemas to publish: %v", err)
	}
	name := regexp.MustCompile(`^[a-z-]+\.v[0-9]+(-provisional)?\.schema\.json$`)
	for _, path := range names {
		base := filepath.Base(path)
		if !name.MatchString(base) {
			t.Errorf("%s: want <surface>.v<N>[-provisional].schema.json", base)
		}
		source, err := os.ReadFile(path) //nolint:gosec // repository file
		if err != nil {
			t.Fatal(err)
		}
		published, err := os.ReadFile(filepath.Join(site, "schemas", base)) //nolint:gosec // test build artifact
		if err != nil {
			t.Errorf("%s is not published: %v", base, err)
			continue
		}
		if !bytes.Equal(source, published) {
			t.Errorf("published %s differs from schemas/%s", base, base)
		}
	}
}
