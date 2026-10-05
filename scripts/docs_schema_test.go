package scripts_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var docsSite = flag.String("docs-site", "", "already built documentation site")

// TestDocsSchema checks the actual build artifact, not a second tracked schema.
func TestDocsSchema(t *testing.T) {
	site := *docsSite
	if site == "" {
		site = t.TempDir()
		cmd := exec.CommandContext(t.Context(), "make", "docs-build", "DOCS_DEST="+site) //nolint:gosec // destination is created by t.TempDir
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
			t.Fatalf("build documentation: %v\n%s", err, out)
		}
	}
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
