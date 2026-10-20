// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package dotfiles

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"golang.org/x/sys/unix"
)

func nativeFixture(t *testing.T, old string) (string, *nativeFS, catalogue.Config, map[string][]byte) {
	t.Helper()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".config"), 0755); err != nil {
		t.Fatal(err)
	}
	if old != "" {
		if err := os.WriteFile(filepath.Join(home, ".config/starship.toml"), []byte(old), 0644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := openNative(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	b := []byte("new = true\n")
	c := catalogue.Config{Marker: "dotfile", Target: ".config/starship.toml", ContentFile: "starship.toml", SHA256: digest(b), Mode: "0644"}
	return home, f, c, map[string][]byte{c.ContentFile: b}
}
func nativeRead(t *testing.T, home, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, p))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func nativeTree(t *testing.T, home string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, p)
		if err != nil {
			return err
		}
		s := info.Mode().String() + "|" + info.ModTime().String()
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			s += "|" + string(b)
		}
		out[rel] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestNativeDotfileLifecycle(t *testing.T) {
	home, _, c, m := nativeFixture(t, "original\n")
	e, close, err := Open(home, fixedClock{})
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	p := preview(t, e, c, m)
	r := apply(t, e, p)
	if r.Status != "applied" || r.Backup == nil || string(nativeRead(t, home, c.Target)) != "new = true\n" {
		t.Fatal(r)
	}
	if string(nativeRead(t, home, strings.TrimPrefix(r.Backup.Path, "~/"))) != "original\n" {
		t.Fatal("backup bytes")
	}
	for _, p := range []string{strings.TrimPrefix(r.Backup.Path, "~/"), ".local/state/whr/backups/0123456789abcdef/manifest.json"} {
		info, err := os.Lstat(filepath.Join(home, p))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private file mode", p, info, err)
		}
	}
	for _, p := range []string{".local/state/whr", ".local/state/whr/backups", ".local/state/whr/backups/0123456789abcdef"} {
		info, err := os.Lstat(filepath.Join(home, p))
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("private dir mode", p, info, err)
		}
	}
	before := nativeTree(t, home)
	r, err = e.Apply(preview(t, e, c, m), Options{})
	if err != nil || r.Status != "unchanged" || !reflect.DeepEqual(before, nativeTree(t, home)) {
		t.Fatal("identical rerun changed tree", r, err)
	}
}
func TestNativeDryRunLeavesTreeUnchanged(t *testing.T) {
	for _, old := range []string{"", "original\n"} {
		home, f, c, m := nativeFixture(t, old)
		if old == "" {
			c.Target = ".config/fish/conf.d/whr.fish"
		}
		e := New(f, fixedClock{})
		before := nativeTree(t, home)
		p := preview(t, e, c, m)
		if _, err := e.Apply(p, Options{DryRun: true}); err != nil || !reflect.DeepEqual(before, nativeTree(t, home)) {
			t.Fatal("dry-run changed files/modes/timestamps", err)
		}
		if old == "" {
			if _, err := e.Apply(p, Options{Decision: Create, Target: c.Target, RunID: "create"}); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{".config/fish", ".config/fish/conf.d"} {
				info, err := os.Lstat(filepath.Join(home, dir))
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatal("missing parent mode", dir, err)
				}
			}
			if string(nativeRead(t, home, c.Target)) != "new = true\n" {
				t.Fatal("new file content")
			}
		}
	}
}
func TestNativeNoFollowAndSpecialRefusals(t *testing.T) {
	for _, component := range []string{".config", ".config/fish", ".config/fish/conf.d", ".config/fish/conf.d/whr.fish"} {
		home, f, c, m := nativeFixture(t, "")
		c.Target = ".config/fish/conf.d/whr.fish"
		outside := t.TempDir()
		canary := filepath.Join(outside, "canary")
		if err := os.WriteFile(canary, []byte("untouched"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(home, component)), 0700); err != nil {
			t.Fatal(err)
		}
		if component == ".config" {
			if err := os.Remove(filepath.Join(home, component)); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(outside, filepath.Join(home, component)); err != nil {
			t.Fatal(err)
		}
		if _, err := New(f, fixedClock{}).Preview(c, m); err == nil {
			t.Fatal("followed link", component)
		}
		if string(nativeRead(t, outside, "canary")) != "untouched" {
			t.Fatal("outside changed")
		}
	}
	home, f, c, m := nativeFixture(t, "")
	if err := unix.Mkfifo(filepath.Join(home, c.Target), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(f, fixedClock{}).Preview(c, m); err == nil {
		t.Fatal("FIFO accepted")
	}

	backupHome, backupFS, backupConfig, backupTemplates := nativeFixture(t, "original\n")
	backupEngine := New(backupFS, fixedClock{})
	previewed := preview(t, backupEngine, backupConfig, backupTemplates)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "canary"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(backupHome, ".local")); err != nil {
		t.Fatal(err)
	}
	if _, err := backupEngine.Apply(previewed, Options{Decision: Replace, Target: backupConfig.Target, RunID: "unsafe"}); err == nil {
		t.Fatal("backup link accepted")
	}
	if string(nativeRead(t, outside, "canary")) != "outside" || string(nativeRead(t, backupHome, backupConfig.Target)) != "original\n" {
		t.Fatal("backup link changed target")
	}
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openNative(link); err == nil {
		f.Close()
		t.Fatal("root symlink accepted")
	}
	for _, p := range []string{"../outside", "/etc/passwd", ".config//x", ".config/./x", ""} {
		if _, err := f.Stat(p); err == nil {
			t.Fatal("invalid native path", p)
		}
	}
}
func TestNativeDescriptorRaceRefusals(t *testing.T) {
	home, f, c, m := nativeFixture(t, "original\n")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "starship.toml"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	f.beforeParentOpen = func(p string) {
		if p == ".config" {
			f.beforeParentOpen = nil
			if err := os.Rename(filepath.Join(home, ".config"), filepath.Join(home, "moved")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(home, ".config")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := New(f, fixedClock{}).Preview(c, m); err == nil {
		t.Fatal("parent open race followed")
	}
	if string(nativeRead(t, outside, "starship.toml")) != "outside" {
		t.Fatal("outside changed")
	}

	for _, link := range []bool{true, false} {
		home, f, c, m := nativeFixture(t, "original\n")
		f.beforeLeafOpen = func(p string) {
			if p == c.Target {
				f.beforeLeafOpen = nil
				if err := os.Rename(filepath.Join(home, c.Target), filepath.Join(home, "old")); err != nil {
					t.Fatal(err)
				}
				if link {
					if err := os.Symlink(filepath.Join(outside, "starship.toml"), filepath.Join(home, c.Target)); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(filepath.Join(home, c.Target), []byte("original\n"), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		if _, err := New(f, fixedClock{}).Preview(c, m); err == nil {
			t.Fatal("leaf identity race accepted", link)
		}
	}
	home, f, c, m = nativeFixture(t, "original\n")
	e := New(f, fixedClock{})
	p := preview(t, e, c, m)
	if err := os.WriteFile(filepath.Join(home, c.Target), []byte("external edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	before := nativeTree(t, home)
	if _, err := e.Apply(p, Options{Decision: Replace, Target: c.Target, RunID: "stale"}); err == nil || !reflect.DeepEqual(before, nativeTree(t, home)) {
		t.Fatal("stale preview changed filesystem", err)
	}

}
func TestNativeExclusiveIdentityAndAtomicity(t *testing.T) {
	home, f, c, _ := nativeFixture(t, "original\n")
	old, err := f.Stat(c.Target)
	if err != nil {
		t.Fatal(err)
	}

	func() {
		previous := unix.Umask(0777)
		defer unix.Umask(previous)
		made, err := f.Create(".config/private", []byte("private"), 0600)
		if err != nil || made.Mode.Perm() != 0600 {
			t.Fatal("file umask affected mode", made, err)
		}
		if err := f.Mkdir(".config/private-dir", 0700); err != nil {
			t.Fatal("directory mode under restrictive umask", err)
		}
	}()
	privateDir, err := f.Stat(".config/private-dir")
	if err != nil || privateDir.Mode.Perm() != 0700 {
		t.Fatal("directory mode", privateDir, err)
	}
	staged := ".config/staged"
	replacement := bytes.Repeat([]byte("new\n"), 4096)
	created, err := f.Create(staged, replacement, 0600)
	if err != nil || created.ID == "" {
		t.Fatal(created, err)
	}
	if id, err := f.Create(staged, []byte("overwrite"), 0600); err == nil || id.ID != "" {
		t.Fatal("exclusive creation failed")
	}
	if err := f.Rename(staged, c.Target, created.ID, "foreign"); err == nil {
		t.Fatal("stale destination identity accepted")
	}
	if err := f.Remove(staged, "foreign"); err == nil {
		t.Fatal("foreign deletion accepted")
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	started := make(chan struct{})
	readErr := make(chan error, 1)
	seenNew := make(chan struct{}, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		first := true
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(filepath.Join(home, c.Target))
			if first {
				close(started)
				first = false
			}
			if bytes.Equal(b, replacement) {
				select {
				case seenNew <- struct{}{}:
				default:
				}
			}
			if err != nil || (!bytes.Equal(b, []byte("original\n")) && !bytes.Equal(b, replacement)) {
				select {
				case readErr <- errors.New("observed non-atomic target"):
				default:
				}
				return
			}
		}
	}()
	<-started
	err = f.Rename(staged, c.Target, created.ID, old.ID)
	select {
	case <-seenNew:
	case <-time.After(2 * time.Second):
		close(stop)
		wg.Wait()
		t.Fatal("reader never observed replacement")
	}
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readErr:
		t.Fatal(err)
	default:
	}
	if !bytes.Equal(nativeRead(t, home, c.Target), replacement) {
		t.Fatal("wrong replacement")
	}
	source, err := f.Create(".config/new-stage", []byte("new"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.beforeRename = func(_, to string) {
		if to == ".config/absent" {
			f.beforeRename = nil
			if err := os.WriteFile(filepath.Join(home, to), []byte("foreign"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.Rename(".config/new-stage", ".config/absent", source.ID, ""); err == nil || string(nativeRead(t, home, ".config/absent")) != "foreign" {
		t.Fatal("absent target race overwritten", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat("."); err == nil {
		t.Fatal("closed root usable")
	}
}

type nativeFault struct {
	FS
	readPath, target string
	renameErrors     int
	foreignStage     bool
}

func (f *nativeFault) Read(p, id string, n int64) ([]byte, error) {
	if p == f.readPath {
		f.readPath = ""
		if f.foreignStage {
			made, err := f.FS.Create(p+"-foreign", []byte("foreign"), 0600)
			if err != nil {
				return nil, err
			}
			if err = f.FS.Rename(p+"-foreign", p, made.ID, id); err != nil {
				return nil, err
			}
		}
		return nil, fs.ErrPermission
	}
	return f.FS.Read(p, id, n)
}
func (f *nativeFault) Rename(from, to, fromID, toID string) error {
	err := f.FS.Rename(from, to, fromID, toID)
	if err == nil && to == f.target && f.renameErrors > 0 {
		f.renameErrors--
		return fs.ErrPermission
	}
	return err
}
func TestNativeRollbackAndStageOwnership(t *testing.T) {
	home, f, c, m := nativeFixture(t, "original\n")
	fault := &nativeFault{FS: f, target: c.Target, renameErrors: 2}
	e := New(fault, fixedClock{})
	r, err := e.Apply(preview(t, e, c, m), Options{Decision: Replace, Target: c.Target, RunID: "native"})
	if err == nil || !r.Restored || r.Status != "rolled_back" || string(nativeRead(t, home, c.Target)) != "original\n" {
		t.Fatal("native rollback failed", r, err)
	}
	for _, foreign := range []bool{false, true} {
		home, f, c, m := nativeFixture(t, "original\n")
		stage := ".local/state/whr/backups/native/manifest.json.whr-stage-native"
		fault := &nativeFault{FS: f, readPath: stage, foreignStage: foreign}
		e := New(fault, fixedClock{})
		if _, err := e.Apply(preview(t, e, c, m), Options{Decision: Replace, Target: c.Target, RunID: "native"}); err == nil {
			t.Fatal("read fault hidden")
		}
		if string(nativeRead(t, home, c.Target)) != "original\n" {
			t.Fatal("failed stage changed target")
		}
		if foreign {
			if string(nativeRead(t, home, stage)) != "foreign" {
				t.Fatal("foreign stage deleted")
			}
		} else {
			if _, err := os.Lstat(filepath.Join(home, stage)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("owned stage retained", err)
			}
			c.Target = ".config/second.toml"
			if err := os.WriteFile(filepath.Join(home, c.Target), []byte("second\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Apply(preview(t, e, c, m), Options{Decision: Replace, Target: c.Target, RunID: "native"}); err != nil {
				t.Fatal("next target blocked", err)
			}
		}
	}
}

// This calls the kernel boundary directly: the destination is occupied after
// any caller-side inspection could have happened. A plain Renameat must fail it.
func TestNativeKernelNoReplacePreservesOccupiedDestination(t *testing.T) {
	home := t.TempDir()
	for name, content := range map[string]string{"source": "staged contents", "target": "foreign contents"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	sourceBefore, err := inspectAt(fd, "source")
	if err != nil {
		t.Fatal(err)
	}
	targetBefore, err := inspectAt(fd, "target")
	if err != nil {
		t.Fatal(err)
	}
	if err := renameAbsent(fd, "source", fd, "target"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("kernel no-replace must refuse occupied destination: %v", err)
	}
	sourceAfter, err := inspectAt(fd, "source")
	if err != nil || !same(sourceBefore, sourceAfter) {
		t.Fatal("source identity changed", err)
	}
	targetAfter, err := inspectAt(fd, "target")
	if err != nil || !same(targetBefore, targetAfter) {
		t.Fatal("destination identity changed", err)
	}
	if string(nativeRead(t, home, "source")) != "staged contents" || string(nativeRead(t, home, "target")) != "foreign contents" {
		t.Fatal("no-replace changed source or destination bytes")
	}
}
