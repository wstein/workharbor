// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStageTrustMatchesCapturedInputs(t *testing.T) {
	src := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, ".devcontainer"), 0700); err != nil {
		t.Fatal(err)
	}
	put := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, name), []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	put(".devcontainer/devcontainer.json", `{"image":"example"}`)
	put("script.sh", "original\n")
	put(".env", "SECRET_SENTINEL")
	put(".devcontainer/host.env", "generated")
	put(".devcontainer/smoke.env", "generated")
	cap, err := captureStageFiles(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()
	if !strings.HasPrefix(cap.Digest, "sha256:") || len(cap.Digest) != 71 {
		t.Fatalf("digest %q", cap.Digest)
	}
	if len(cap.Omitted) != 3 {
		t.Fatalf("omissions: %v", cap.Omitted)
	}
	b, err := os.ReadFile(filepath.Join(cap.Path, "script.sh"))
	if err != nil || string(b) != "original\n" {
		t.Fatalf("capture: %q %v", b, err)
	}
	put("script.sh", "changed\n")
	if err = cap.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "source inputs changed") {
		t.Fatalf("source drift admitted: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(cap.Path, "script.sh"))
	if string(b) != "original\n" {
		t.Fatal("capture aliases source")
	}
	if _, err = os.Stat(filepath.Join(cap.Path, ".env")); !os.IsNotExist(err) {
		t.Fatalf("credential copied: %v", err)
	}
}
func TestLocalStageCaptureRefusesEscapingSymlink(t *testing.T) {
	src := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("FOREIGN_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "input")); err != nil {
		t.Fatal(err)
	}
	if cap, err := captureStageFiles(context.Background(), src, t.TempDir()); err == nil || !strings.Contains(err.Error(), "symlink outside capture") {
		if cap != nil {
			cap.Close()
		}
		t.Fatalf("escape admitted: %v", err)
	}
}
func TestLocalStageCaptureCanonicalAndCancellation(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "input"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	a, err := captureStageFiles(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := captureStageFiles(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.Digest != b.Digest {
		t.Fatalf("private roots changed fingerprint: %s %s", a.Digest, b.Digest)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c, err := captureStageFiles(ctx, src, t.TempDir()); err != context.Canceled {
		if c != nil {
			c.Close()
		}
		t.Fatalf("cancel: %v", err)
	}
}

func TestLocalStageCaptureCleanupRetainsFailedPath(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "input"), []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	cap, err := captureStageFiles(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := cap.Path
	cap.removeTree = func(string) error { return errors.New("injected removal failure") }
	if err = cap.Close(); err == nil || !strings.Contains(err.Error(), "temporary capture retained") {
		t.Fatalf("failed cleanup: %v", err)
	}
	if cap.Path != path {
		t.Fatalf("lost retained path %q want %q", cap.Path, path)
	}
	if _, err = os.Stat(filepath.Join(path, "input")); err != nil {
		t.Fatalf("retained capture disappeared: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(src, "input"))
	if string(b) != "source" {
		t.Fatal("source touched during cleanup")
	}
	cap.removeTree = nil
	if err = cap.Close(); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "" {
		t.Fatalf("successful cleanup did not clear private path: %q", cap.Path)
	}
}
