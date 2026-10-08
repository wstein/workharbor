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

func TestHugoInstallLeavesNoTempDir(t *testing.T) {
	asset := hugoAsset(t)
	srv := t.TempDir()
	cache := filepath.Join(t.TempDir(), "cache")
	if strings.HasSuffix(asset, ".tar.gz") {
		work := t.TempDir()
		if err := os.WriteFile(filepath.Join(work, "hugo"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil { //nolint:gosec // fake executable
			t.Fatal(err)
		}
		if out, err := exec.CommandContext(t.Context(), "tar", "-czf", filepath.Join(srv, asset), "-C", work, "hugo").CombinedOutput(); err != nil { //nolint:gosec // test-controlled paths
			t.Fatalf("%v: %s", err, out)
		}
	} else {
		t.Skip("a signed-free fake .pkg is not practical; covered on Linux")
	}
	sum, err := exec.CommandContext(t.Context(), "shasum", "-a", "256", filepath.Join(srv, asset)).Output() //nolint:gosec // test-controlled paths
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, d := range []string{"scripts", ".config"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil { //nolint:gosec // test fixture
			t.Fatal(err)
		}
	}
	script, _ := os.ReadFile("hugo.sh")
	if err := os.WriteFile(filepath.Join(root, "scripts", "hugo.sh"), script, 0o755); err != nil { //nolint:gosec // copy of the script
		t.Fatal(err)
	}
	line := strings.Fields(string(sum))[0] + "  " + asset + "\n"
	if err := os.WriteFile(filepath.Join(root, ".config", "hugo.sha256"), []byte(line), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(root, "scripts", "hugo.sh")) //nolint:gosec // test-controlled paths
	cmd.Env = append(os.Environ(), "HUGO_CACHE_DIR="+cache, "HUGO_BASE_URL=file://"+srv)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("%v: %s", err, out)
	}
	if m, _ := filepath.Glob(filepath.Join(cache, "tmp.*")); len(m) != 0 {
		t.Fatalf("temp dirs left behind: %v", m)
	}
}
