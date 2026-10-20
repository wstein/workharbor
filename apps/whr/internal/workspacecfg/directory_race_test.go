// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func swapInspectedDirectory(t *testing.T, path string, link bool) {
	t.Helper()
	held := path + "-held"
	if err := os.Rename(path, held); err != nil {
		t.Fatal(err)
	}
	if link {
		if err := os.Symlink(filepath.Base(held), path); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenDirRefusesInspectedDirectorySwap(t *testing.T) {
	for _, link := range []bool{true, false} {
		name := "different_inode"
		if link {
			name = "same_inode_link"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "root")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			calls := 0
			d, err := openDirBeforeOpen(path, func() { calls++; swapInspectedDirectory(t, path, link) })
			if d != nil {
				d.Close()
			}
			var unsafe *accessError
			if calls != 1 || !errors.As(err, &unsafe) || unsafe.kind != accUnsafe {
				t.Fatalf("actor=%d dir=%v err=%v; want unsafe refusal", calls, d, err)
			}
		})
	}
}

func TestWalkRefusesInspectedDirectorySwap(t *testing.T) {
	for _, link := range []bool{true, false} {
		name := "different_inode"
		if link {
			name = "same_inode_link"
		}
		t.Run(name, func(t *testing.T) {
			d, path := leafRaceDir(t)
			nested := filepath.Join(path, "nested")
			if err := os.Mkdir(nested, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(nested, "leaf"), []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			d.beforeDirOpen = func(rel string) {
				if rel == "nested" {
					calls++
					swapInspectedDirectory(t, nested, link)
				}
			}
			data, err := d.readFile("nested/leaf", 64)
			if calls != 1 || len(data) != 0 || err == nil || err.kind != accUnsafe {
				t.Fatalf("actor=%d bytes=%q err=%v; want unsafe refusal", calls, data, err)
			}
		})
	}
}

func TestReadDirRefusesInspectedDirectorySwap(t *testing.T) {
	for _, link := range []bool{true, false} {
		name := "different_inode"
		if link {
			name = "same_inode_link"
		}
		t.Run(name, func(t *testing.T) {
			d, path := leafRaceDir(t)
			listed := filepath.Join(path, "parent/listed")
			if err := os.MkdirAll(listed, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(listed, "original"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			d.beforeDirOpen = func(rel string) {
				if rel == "parent/listed" {
					calls++
					swapInspectedDirectory(t, listed, link)
				}
			}
			entries, err := d.readDir("parent/listed", 10)
			if calls != 1 || len(entries) != 0 || err == nil || err.kind != accUnsafe {
				t.Fatalf("actor=%d entries=%v err=%v; want unsafe refusal", calls, entries, err)
			}
		})
	}
}

func TestDirectoryWalkUsesHeldNestedParent(t *testing.T) {
	d, path := leafRaceDir(t)
	parent := filepath.Join(path, "parent")
	if err := os.MkdirAll(filepath.Join(parent, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "child/leaf"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	d.beforeDirOpen = func(rel string) {
		if rel != "parent/child" {
			return
		}
		calls++
		swapInspectedDirectory(t, parent, false)
		if err := os.Mkdir(filepath.Join(parent, "child"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, "child/leaf"), []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := d.readFile("parent/child/leaf", 64)
	if calls != 1 || err != nil || string(data) != "original" {
		t.Fatalf("actor=%d bytes=%q err=%v; held parent was lost", calls, data, err)
	}
}

func TestReadDirEntriesNeverReadReplacementPath(t *testing.T) {
	d, path := leafRaceDir(t)
	listed := filepath.Join(path, "listed")
	if err := os.Mkdir(listed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(listed, "entry"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := d.readDir("listed", 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	swapInspectedDirectory(t, listed, false)
	if err := os.WriteFile(filepath.Join(listed, "entry"), []byte("replacement-path"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, infoErr := entries[0].Info()
	if infoErr != nil {
		t.Fatalf("snapshot metadata unavailable: %v", infoErr)
	}
	if info.Size() != int64(len("original")) || info.Mode().Perm() != 0o600 {
		t.Fatalf("entry.Info read replacement: size=%d mode=%v", info.Size(), info.Mode())
	}
	if entries[0].Name() != "entry" || entries[0].IsDir() || entries[0].Type() != 0 {
		t.Fatalf("entry type/name changed: %v", entries[0])
	}
}

func TestNativeDirectoryMetadataModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		info nativeFileInfo
		want fs.FileMode
	}{
		{"regular", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFREG | 0640}}, 0640},
		{"directory", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFDIR | 0750}}, fs.ModeDir | 0750},
		{"symlink", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFLNK | 0777}}, fs.ModeSymlink | 0777},
		{"fifo", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFIFO | 0600}}, fs.ModeNamedPipe | 0600},
		{"socket", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFSOCK | 0600}}, fs.ModeSocket | 0600},
		{"character", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFCHR | 0600}}, fs.ModeDevice | fs.ModeCharDevice | 0600},
		{"block", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFBLK | 0600}}, fs.ModeDevice | 0600},
		{"special bits", nativeFileInfo{stat: unix.Stat_t{Mode: unix.S_IFREG | unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX | 0700}}, fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0700},
		{"unknown", nativeFileInfo{stat: unix.Stat_t{Mode: 0600}}, fs.ModeIrregular | 0600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.Mode(); got != tc.want {
				t.Fatalf("mode %v, want %v", got, tc.want)
			}
		})
	}
	info := nativeFileInfo{name: "entry", stat: unix.Stat_t{Mode: unix.S_IFREG, Size: 17, Mtim: unix.Timespec{Sec: 123, Nsec: 456}}}
	if info.Name() != "entry" || info.Size() != 17 || info.IsDir() || !info.ModTime().Equal(time.Unix(123, 456)) {
		t.Fatalf("metadata fields: %+v", info)
	}
	if _, ok := info.Sys().(*unix.Stat_t); !ok {
		t.Fatalf("unexpected native metadata type %T", info.Sys())
	}
}

func TestNativeDirectoryIdentityUsesDeviceAndInode(t *testing.T) {
	original := &nativeFileInfo{stat: unix.Stat_t{Dev: 1, Ino: 2}}
	for _, tc := range []struct {
		name  string
		other nativeFileInfo
		want  bool
	}{
		{"same", nativeFileInfo{stat: unix.Stat_t{Dev: 1, Ino: 2, Size: 99}}, true},
		{"device", nativeFileInfo{stat: unix.Stat_t{Dev: 3, Ino: 2}}, false},
		{"inode", nativeFileInfo{stat: unix.Stat_t{Dev: 1, Ino: 3}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameNativeFile(original, &tc.other); got != tc.want {
				t.Fatalf("identity=%v,want %v", got, tc.want)
			}
		})
	}
}

func TestNativeDirectoryLstatNeverFollowsLeafLink(t *testing.T) {
	d, path := leafRaceDir(t)
	if err := os.Symlink("leaf", filepath.Join(path, "link")); err != nil {
		t.Fatal(err)
	}
	info, err := d.root.Lstat("link")
	if err != nil || info.Mode()&fs.ModeSymlink == 0 || info.Mode().IsRegular() {
		t.Fatalf("native lstat followed link: info=%+v err=%v", info, err)
	}
}

func TestDirectoryHandlesDoNotLeakFDs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-count proof uses Linux /proc/self/fd; Darwin receives test-compiler verification")
	}
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "nested/leaf"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	exercise := func(i int) {
		d, err := openDir(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.readFile("nested/leaf", 64); err != nil {
			d.Close()
			t.Fatal(err)
		}
		if _, err := d.readDir("nested", 1); err != nil {
			d.Close()
			t.Fatal(err)
		}
		if _, err := d.readFile("nested/missing/leaf", 64); err == nil {
			d.Close()
			t.Fatal("missing component accepted")
		}
		d.Close()
		replaced := filepath.Join(path, fmt.Sprintf("failed-%d", i))
		if err := os.Mkdir(replaced, 0o700); err != nil {
			t.Fatal(err)
		}
		bad, err := openDirBeforeOpen(replaced, func() { swapInspectedDirectory(t, replaced, false) })
		if bad != nil {
			bad.Close()
		}
		if err == nil {
			t.Fatal("root replacement accepted")
		}
	}
	exercise(-1) // warm up standard-library descriptor handling before counting
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		exercise(i)
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("descriptor count before=%d after=%d", len(before), len(after))
	}
}
