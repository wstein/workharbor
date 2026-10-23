// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeDocker(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestLocalDockerArgvIsFixedAndEndpointPinned(t *testing.T) {
	exe := fakeDocker(t, `printf '%s\n' "$@"`)
	c := Client{Executable: exe}
	raw, err := c.read(context.Background(), "unix:///test/socket", "container", "ls", "--all", "--no-trunc", "--format", "{{json .}}")
	want := "--host=unix:///test/socket\ncontainer\nls\n--all\n--no-trunc\n--format\n{{json .}}\n"
	if err != nil || string(raw) != want {
		t.Fatalf("%q %v", raw, err)
	}
}
func TestLocalDockerClientEnvironmentExcludesTokens(t *testing.T) {
	for _, k := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GITHUB_TOKEN", "DOCKER_CONTEXT", "DOCKER_AUTH_CONFIG", "DOCKER_CERT_PATH", "DOCKER_HOST", "DOCKER_TLS_VERIFY"} {
		t.Setenv(k, "canary-secret")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	exe := fakeDocker(t, `/usr/bin/env; test -d "$DOCKER_CONFIG" && test ! -e "$DOCKER_CONFIG/config.json"`)
	raw, err := (Client{Executable: exe}).read(context.Background(), "unix:///test/socket", "container", "ls", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil || strings.Contains(string(raw), "canary-secret") {
		t.Fatalf("environment isolation failed: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		k, _, _ := strings.Cut(line, "=")
		if k != "PATH" && k != "DOCKER_CONFIG" && k != "LC_ALL" && k != "PWD" {
			t.Fatalf("unexpected child variable %s", k)
		}
	}
}
func TestLocalDiscoveryCancellationStopsChild(t *testing.T) {
	exe := fakeDocker(t, `/bin/sleep 60 & wait`)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := (Client{Executable: exe}).read(ctx, "unix:///test/socket", "container", "ls", "--all", "--no-trunc", "--format", "{{json .}}")
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("child not cancelled promptly: %v", err)
	}
}
func TestLocalDockerOutputBound(t *testing.T) {
	exe := fakeDocker(t, `/usr/bin/head -c 4194305 /dev/zero; /bin/sleep 60`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := (Client{Executable: exe}).read(ctx, "unix:///test/socket", "container", "ls", "--all", "--no-trunc", "--format", "{{json .}}")
	if err == nil || !strings.Contains(err.Error(), "exceeds discovery limit") {
		t.Fatalf("output bound did not fire: %v", err)
	}
}

// Independent literal: a production-format mutation must not change the oracle.
const expectedInspectFormat = `{"Id":{{json .Id}},"Name":{{json .Name}},"Image":{{json .Config.Image}},"State":{{json .State.Status}},"Labels":{{json .Config.Labels}},"Mounts":{{json .Mounts}}}`

func TestLocalDockerInspectArgvOmitsEnvironment(t *testing.T) {
	exe := fakeDocker(t, `printf '%s\n' "$@"`)
	id := strings.Repeat("a", 64)
	raw, err := (Client{Executable: exe}).read(context.Background(), "unix:///test/socket", "container", "inspect", "--format", inspectFormat, id)
	want := "--host=unix:///test/socket\ncontainer\ninspect\n--format\n" + expectedInspectFormat + "\n" + id + "\n"
	if err != nil || string(raw) != want {
		t.Fatalf("inspect must request only approved fields: %q %v", raw, err)
	}
}
