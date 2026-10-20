// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func leafRaceDir(t *testing.T) (*dir, string) {
	t.Helper()
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "leaf"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := openDir(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, path
}

func TestReadFileRefusesLeafIdentityReplacement(t *testing.T) {
	d, path := leafRaceDir(t)
	calls := 0
	d.beforeFileOpen = func() {
		calls++
		// Replace a regular file with another regular inode after inspection.
		// O_NOFOLLOW alone cannot distinguish these two files.
		if err := os.Rename(filepath.Join(path, "leaf"), filepath.Join(path, "original")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "leaf"), []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := d.readFile("leaf", 64)
	if calls != 1 {
		t.Fatalf("replacement actor ran %d times, want once", calls)
	}
	if len(data) != 0 || err == nil || err.kind != accUnsafe || err.why != "changed while it was opened" {
		t.Fatalf("replacement read: bytes=%q error=%+v; want identity refusal before reading", data, err)
	}
}

func TestReadFileNoFollowRefusesLinkToInspectedInode(t *testing.T) {
	d, path := leafRaceDir(t)
	calls := 0
	d.beforeFileOpen = func() {
		calls++
		// Preserve the inspected inode, then link back to it within the root.
		// SameFile still succeeds if the open follows this link.
		if err := os.Rename(filepath.Join(path, "leaf"), filepath.Join(path, "original")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("original", filepath.Join(path, "leaf")); err != nil {
			t.Fatal(err)
		}
	}
	data, err := d.readFile("leaf", 64)
	if calls != 1 {
		t.Fatalf("replacement actor ran %d times, want once", calls)
	}
	if len(data) != 0 || err == nil || err.kind != accUnsafe || err.why != "cannot be opened" {
		t.Fatalf("link read: bytes=%q error=%+v; want no-follow refusal before reading", data, err)
	}
}

// Renaming a nested parent must not redirect the leaf opener to a replacement
// at the original pathname. The inspected descriptor still owns the old parent.
func TestReadFileLeafOpenUsesHeldParent(t *testing.T) {
	d, path := leafRaceDir(t)
	if err := os.Mkdir(filepath.Join(path, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "nested/leaf"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	d.beforeFileOpen = func() {
		calls++
		if err := os.Rename(filepath.Join(path, "nested"), filepath.Join(path, "held")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(path, "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "nested/leaf"), []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := d.readFile("nested/leaf", 64)
	if calls != 1 {
		t.Fatalf("replacement actor ran %d times, want once", calls)
	}
	if err != nil || string(data) != "original" {
		t.Fatalf("held parent read: bytes=%q error=%+v", data, err)
	}
}

func TestOpenLeafNoFollowReturnsBlockingFile(t *testing.T) {
	d, _ := leafRaceDir(t)
	f, err := openLeafNoFollow(d.root, "leaf")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	raw, err := f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	var flagErr error
	if err := raw.Control(func(fd uintptr) { flags, flagErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if flagErr != nil {
		t.Fatal(flagErr)
	}
	if flags&unix.O_NONBLOCK != 0 {
		t.Fatalf("returned file retains O_NONBLOCK: flags=%#x", flags)
	}
}

func TestOpenLeafNoFollowRefusesNonLeafNames(t *testing.T) {
	d, _ := leafRaceDir(t)
	for _, name := range []string{"", ".", "..", "nested/leaf"} {
		f, err := openLeafNoFollow(d.root, name)
		if f != nil {
			f.Close()
			t.Errorf("non-leaf %q returned a file", name)
		}
		if err == nil {
			t.Errorf("non-leaf %q was accepted", name)
		}
	}
}
