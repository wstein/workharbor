// SPDX-License-Identifier: EUPL-1.2

//go:build linux || darwin || freebsd

package doctor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

// F4: the real bounded reader.
func TestDoctorReadBounded(t *testing.T) {
	read := doctor.SystemPorts(nil).ReadFile
	dir := t.TempDir()
	const max = 64 << 10
	write := func(name string, n int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, bytes.Repeat([]byte("a"), n), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if b, err := read(write("exact", max), max); err != nil || len(b) != max {
		t.Errorf("exactly the bound: %d %v", len(b), err)
	}
	if _, err := read(write("over", max+1), max); err == nil {
		t.Error("one byte over the bound was accepted")
	}
	sparse := filepath.Join(dir, "sparse")
	f, err := os.Create(sparse)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(10 << 30); err != nil {
		t.Skipf("filesystem refuses a sparse file: %v", err)
	}
	f.Close()
	if _, err := read(sparse, max); err == nil {
		t.Error("sparse 10 GiB file accepted")
	}
	target := write("target", 3)
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := read(filepath.Join(dir, "link"), max); err == nil {
		t.Error("symlink followed")
	}
	if err := os.Symlink("loop", filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}
	if _, err := read(filepath.Join(dir, "loop"), max); err == nil {
		t.Error("symlink loop accepted")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := read(fifo, max); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("FIFO accepted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
	if _, err := read(dir, max); err == nil {
		t.Error("directory accepted")
	}
}
