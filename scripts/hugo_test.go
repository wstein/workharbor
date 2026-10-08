package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hugoAsset returns the pinned asset name hugo.sh picks on this host.
func hugoAsset(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS + "-" + runtime.GOARCH {
	case "linux-amd64":
		return "hugo_extended_0.165.0_linux-amd64.tar.gz"
	case "linux-arm64":
		return "hugo_extended_0.165.0_linux-arm64.tar.gz"
	case "darwin-amd64", "darwin-arm64":
		return "hugo_extended_0.165.0_darwin-universal.pkg"
	}
	t.Skip("no pinned Hugo asset for this platform")
	return ""
}

func TestHugoPinnedChecksumsCoverEveryPlatform(t *testing.T) {
	data, err := os.ReadFile("../.config/hugo.sha256")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{
		"hugo_extended_0.165.0_linux-amd64.tar.gz",
		"hugo_extended_0.165.0_linux-arm64.tar.gz",
		"hugo_extended_0.165.0_darwin-universal.pkg",
	} {
		if !strings.Contains(string(data), "  "+a+"\n") {
			t.Errorf("no checksum for %s", a)
		}
	}
}

func TestHugoRefusesAssetWithWrongChecksum(t *testing.T) {
	asset := hugoAsset(t)
	srv := t.TempDir()
	if err := os.WriteFile(filepath.Join(srv, asset), []byte("not hugo"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "cache")
	cmd := exec.CommandContext(t.Context(), "bash", "hugo.sh", "version")
	cmd.Env = append(os.Environ(), "HUGO_CACHE_DIR="+cache, "HUGO_BASE_URL=file://"+srv)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a tampered asset was accepted: %s", out)
	}
	if _, statErr := os.Stat(filepath.Join(cache, "v0.165.0", "hugo")); statErr == nil {
		t.Fatal("an unverified binary was installed")
	}
	if !strings.Contains(string(out), "FAILED") {
		t.Fatalf("want a checksum failure, got: %s", out)
	}
}

func TestHugoRunsCachedBinaryWithoutDownload(t *testing.T) {
	hugoAsset(t)
	cache := t.TempDir()
	dir := filepath.Join(cache, "v0.165.0")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hugo"), []byte("#!/bin/sh\necho cached \"$@\"\n"), 0o755); err != nil { //nolint:gosec // fake executable
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "hugo.sh", "--gc")
	cmd.Env = append(os.Environ(), "HUGO_CACHE_DIR="+cache, "HUGO_BASE_URL=file:///nonexistent")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "cached --gc" {
		t.Fatalf("got %q, %v", out, err)
	}
}
