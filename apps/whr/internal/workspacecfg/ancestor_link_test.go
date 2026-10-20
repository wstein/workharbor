// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenDirAcceptsSymlinkedAncestor(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real/root")
	if err := os.MkdirAll(filepath.Join(real, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "nested/leaf"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(base, "alink")); err != nil {
		t.Fatal(err)
	}
	// The chosen root is a real directory; only an ancestor is a link. This is
	// also the policy needed for macOS /tmp and symlinked home directories.
	d, err := openDir(filepath.Join(base, "alink/root"))
	if err != nil {
		t.Fatalf("ancestor link rejected: %v", err)
	}
	defer d.Close()
	data, readErr := d.readFile("nested/leaf", 64)
	if readErr != nil || string(data) != "original" {
		t.Fatalf("nested read through accepted ancestor: bytes=%q err=%v", data, readErr)
	}
	if !d.stillAtAbs() {
		t.Fatal("absolute-path identity check rejected the accepted ancestor link")
	}
}
