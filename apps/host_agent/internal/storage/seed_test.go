// SPDX-License-Identifier: EUPL-1.2
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (Store, Binding, *config.Resolved) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(root, 0700)
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "binary"), []byte{0, 1, 255, 128}, 0600)
	os.Mkdir(filepath.Join(source, "dir"), 0755)
	os.Symlink("../binary", filepath.Join(source, "dir/link"))
	d := config.Descriptor{RepositoryIdentity: "https://example.test/config.git", Commit: strings.Repeat("a", 40), Submodules: []config.Submodule{}}
	b := Binding{OwnerID: "00000000-0000-4000-8000-000000000001", HostID: "00000000-0000-4000-8000-000000000002", WorkspaceID: "00000000-0000-4000-8000-000000000003", StorageID: "00000000-0000-4000-8000-000000000004", ReservationOperationID: "00000000-0000-4000-8000-000000000005", Initial: d, DataUID: uint32(os.Geteuid()), DataGID: uint32(os.Getegid())}
	return Store{Root: root}, b, &config.Resolved{ProjectsPath: source, Descriptor: d}
}
func allow(context.Context) error { return nil }
func assertCode(t *testing.T, e error, code string) {
	t.Helper()
	var err *config.Error
	if !errors.As(e, &err) || err.Code != code {
		t.Fatalf("got %v want %s", e, code)
	}
}
func TestSeedPublishesWholePrivateParentAndRetainsEditedBinaryUntracked(t *testing.T) {
	s, b, source := fixture(t)
	h, e := s.Seed(context.Background(), b, source, allow)
	if e != nil {
		t.Fatal(e)
	}
	if h.Parent != filepath.Join(s.Root, b.StorageID) || h.Data != filepath.Join(h.Parent, "data") {
		t.Fatal("handle mismatch")
	}
	info, e := os.Stat(h.Parent)
	if e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("parent not private")
	}
	if _, e := os.Stat(filepath.Join(h.Parent, "seed.json")); e != nil {
		t.Fatal(e)
	}
	edit := []byte{0, 9, 255, 0, 42}
	os.WriteFile(filepath.Join(h.Data, "binary"), edit, 0640)
	os.WriteFile(filepath.Join(h.Data, "untracked"), []byte("keep me"), 0600)
	b.ExistingHandle = h.Parent
	source.Descriptor.Commit = strings.Repeat("b", 40)
	second, e := s.Seed(context.Background(), b, source, allow)
	if e != nil || second.Parent != h.Parent {
		t.Fatal("valid original seed not reused", e)
	}
	raw, e := os.ReadFile(filepath.Join(h.Data, "binary"))
	if e != nil || sha256.Sum256(raw) != sha256.Sum256(edit) {
		t.Fatal("binary reset")
	}
	if raw, e := os.ReadFile(filepath.Join(h.Data, "untracked")); e != nil || string(raw) != "keep me" {
		t.Fatal("untracked reset")
	}
}
func TestSeedKnownHandleMissingAndWrongOriginalNeverCopies(t *testing.T) {
	s, b, source := fixture(t)
	b.ExistingHandle = filepath.Join(s.Root, b.StorageID)
	_, e := s.Seed(context.Background(), b, source, allow)
	assertCode(t, e, "storage_unavailable")
	if _, e := os.Stat(b.ExistingHandle); !os.IsNotExist(e) {
		t.Fatal("known missing handle was repaired")
	}
	b.ExistingHandle = ""
	source.Descriptor.Commit = strings.Repeat("b", 40)
	_, e = s.Seed(context.Background(), b, source, allow)
	assertCode(t, e, "seed_mismatch")
}
func TestSeedAuthorizationPrecedesAnyFilesystemEffect(t *testing.T) {
	s, b, source := fixture(t)
	denied := errors.New("denied")
	_, e := s.Seed(context.Background(), b, source, func(context.Context) error { return denied })
	if !errors.Is(e, denied) {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(s.Root)
	if e != nil || len(entries) != 0 {
		t.Fatal("unauthorized storage effect")
	}
}
func TestSeedInvalidFinalPreservedAndSymlinkEscapeRefused(t *testing.T) {
	s, b, source := fixture(t)
	final := filepath.Join(s.Root, b.StorageID)
	os.Mkdir(final, 0700)
	os.WriteFile(filepath.Join(final, "canary"), []byte("retained"), 0600)
	_, e := s.Seed(context.Background(), b, source, allow)
	assertCode(t, e, "storage_conflict")
	if raw, e := os.ReadFile(filepath.Join(final, "canary")); e != nil || string(raw) != "retained" {
		t.Fatal("foreign data changed")
	}
	s, b, source = fixture(t)
	os.Symlink("/etc/passwd", filepath.Join(source.ProjectsPath, "escape"))
	_, e = s.Seed(context.Background(), b, source, allow)
	assertCode(t, e, "invalid_seed")
	if _, e := os.Stat(filepath.Join(s.Root, b.StorageID)); !os.IsNotExist(e) {
		t.Fatal("partial published")
	}
}
func TestSeedCrashBoundariesNeverPublishPartialAndRetryRetains(t *testing.T) {
	for _, point := range []string{"owned_temp", "copy_file", "copy_complete", "manifest_synced", "before_rename", "after_rename"} {
		t.Run(point, func(t *testing.T) {
			s, b, source := fixture(t)
			interrupted := errors.New("interrupted")
			s.checkpoint = func(p string) error {
				if p == point {
					return interrupted
				}
				return nil
			}
			_, e := s.Seed(context.Background(), b, source, allow)
			if !errors.Is(e, interrupted) {
				t.Fatal(e)
			}
			final := filepath.Join(s.Root, b.StorageID)
			if point != "after_rename" {
				if _, e := os.Stat(final); !os.IsNotExist(e) {
					t.Fatal("partial final visible")
				}
			}
			s.checkpoint = nil
			h, e := s.Seed(context.Background(), b, source, allow)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := os.ReadFile(filepath.Join(h.Data, "binary"))
			if e != nil || string(raw) != string([]byte{0, 1, 255, 128}) {
				t.Fatal("incomplete copy")
			}
		})
	}
}
func TestSeedCompetingPublishersHaveOnePersistentFinal(t *testing.T) {
	s, b, source := fixture(t)
	var wg sync.WaitGroup
	results := make(chan Handle, 4)
	failures := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			h, e := s.Seed(context.Background(), b, source, allow)
			if e != nil {
				failures <- e
			} else {
				results <- h
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	if len(results) != 4 {
		t.Fatal("publisher failed")
	}
	for h := range results {
		if h.Parent != filepath.Join(s.Root, b.StorageID) {
			t.Fatal("multiple final parents")
		}
	}
}

func TestSeedForeignPublisherCollisionNeverOverwrites(t *testing.T) {
	s, b, source := fixture(t)
	final := filepath.Join(s.Root, b.StorageID)
	s.checkpoint = func(p string) error {
		if p == "before_rename" {
			os.Mkdir(final, 0700)
			os.WriteFile(filepath.Join(final, "foreign"), []byte("keep"), 0600)
		}
		return nil
	}
	_, e := s.Seed(context.Background(), b, source, allow)
	assertCode(t, e, "storage_conflict")
	raw, e := os.ReadFile(filepath.Join(final, "foreign"))
	if e != nil || string(raw) != "keep" {
		t.Fatal("foreign final overwritten")
	}
}
func TestSeedInitialModesOwnershipAndChangedOwnerRefusal(t *testing.T) {
	s, b, source := fixture(t)
	os.WriteFile(filepath.Join(source.ProjectsPath, "executable"), []byte("fixture"), 0755)
	os.Mkdir(filepath.Join(source.ProjectsPath, "readonly"), 0700)
	os.WriteFile(filepath.Join(source.ProjectsPath, "readonly/file"), []byte("keep"), 0644)
	os.Chmod(filepath.Join(source.ProjectsPath, "readonly"), 0555)
	h, e := s.Seed(context.Background(), b, source, allow)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(h.Data, "executable"))
	if e != nil || info.Mode().Perm() != 0755 {
		t.Fatal("executable mode lost")
	}
	t.Cleanup(func() {
		os.Chmod(filepath.Join(source.ProjectsPath, "readonly"), 0700)
		os.Chmod(filepath.Join(h.Data, "readonly"), 0700)
	})
	info, e = os.Stat(filepath.Join(h.Data, "readonly"))
	if e != nil || info.Mode().Perm() != 0555 {
		t.Fatal("readonly directory mode lost")
	}
	b.DataGID++
	_, e = s.Seed(context.Background(), b, source, allow)
	if e == nil {
		t.Fatal("changed initial owner adopted")
	}
}

// This helper is test-only. Production storage has no environment crash switch.
func TestSeedProcessHelper(t *testing.T) {
	if os.Getenv("WHR_SEED_HELPER") != "1" {
		return
	}
	raw, e := base64.StdEncoding.DecodeString(os.Getenv("WHR_SEED_BINDING"))
	if e != nil {
		t.Fatal(e)
	}
	var b Binding
	if json.Unmarshal(raw, &b) != nil {
		t.Fatal("helper binding")
	}
	root, source, point := os.Getenv("WHR_SEED_ROOT"), os.Getenv("WHR_SEED_SOURCE"), os.Getenv("WHR_SEED_POINT")
	s := Store{Root: root}
	s.checkpoint = func(p string) error {
		if p == point {
			process, e := os.FindProcess(os.Getpid())
			if e != nil {
				t.Fatal(e)
			}
			process.Kill()
			select {}
		}
		return nil
	}
	if ready := os.Getenv("WHR_SEED_READY"); ready != "" {
		os.WriteFile(ready, []byte("ready"), 0600)
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, e := os.Stat(os.Getenv("WHR_SEED_START")); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("helper barrier deadline")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	_, e = s.Seed(context.Background(), b, &config.Resolved{ProjectsPath: source, Descriptor: b.Initial}, allow)
	if e != nil {
		t.Fatal(e)
	}
}
func helper(ctx context.Context, s Store, b Binding, source *config.Resolved, point string) *exec.Cmd {
	raw, _ := json.Marshal(b)
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSeedProcessHelper$")
	c.Env = []string{"WHR_SEED_HELPER=1", "WHR_SEED_BINDING=" + base64.StdEncoding.EncodeToString(raw), "WHR_SEED_ROOT=" + s.Root, "WHR_SEED_SOURCE=" + source.ProjectsPath, "WHR_SEED_POINT=" + point}
	return c
}
func TestSeedSIGKILLBoundariesRecoverWithoutPartialPublicationOrReset(t *testing.T) {
	for _, point := range []string{"owned_temp", "copy_file", "copy_complete", "manifest_synced", "before_rename", "after_rename"} {
		t.Run(point, func(t *testing.T) {
			s, b, source := fixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := helper(ctx, s, b, source, point)
			e := c.Run()
			var exit *exec.ExitError
			if !errors.As(e, &exit) || ctx.Err() != nil || exit.ProcessState.ExitCode() != -1 {
				t.Fatalf("helper was not killed at checkpoint: %v", e)
			}
			final := filepath.Join(s.Root, b.StorageID)
			if point != "after_rename" {
				if _, e := os.Lstat(final); !os.IsNotExist(e) {
					t.Fatal("partial final exposed")
				}
			} else {
				os.WriteFile(filepath.Join(final, "data/untracked"), []byte("retained-after-crash"), 0600)
			}
			h, e := s.Seed(context.Background(), b, source, allow)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := os.ReadFile(filepath.Join(h.Data, "binary"))
			if e != nil || string(raw) != string([]byte{0, 1, 255, 128}) {
				t.Fatal("partial data recovered")
			}
			if point == "after_rename" {
				raw, e := os.ReadFile(filepath.Join(h.Data, "untracked"))
				if e != nil || string(raw) != "retained-after-crash" {
					t.Fatal("completed final recopied")
				}
			}
		})
	}
}
func TestSeedIndependentProcessesSerializeOneFinal(t *testing.T) {
	s, b, source := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := filepath.Join(t.TempDir(), "start")
	commands := []*exec.Cmd{}
	for _, name := range []string{"one", "two"} {
		c := helper(ctx, s, b, source, "")
		ready := filepath.Join(filepath.Dir(start), name)
		c.Env = append(c.Env, "WHR_SEED_READY="+ready, "WHR_SEED_START="+start)
		if e := c.Start(); e != nil {
			t.Fatal(e)
		}
		commands = append(commands, c)
	}
	for _, name := range []string{"one", "two"} {
		for {
			if _, e := os.Stat(filepath.Join(filepath.Dir(start), name)); e == nil {
				break
			}
			if ctx.Err() != nil {
				t.Fatal("helper startup deadline")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	os.WriteFile(start, []byte("go"), 0600)
	for _, c := range commands {
		if e := c.Wait(); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Seed(context.Background(), b, source, allow); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(s.Root)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".seed-") {
			t.Fatal("owned incomplete stage left")
		}
	}
}

func TestSeedActualSymlinkChainsNeverEscapeDataOrRemainIndeterminate(t *testing.T) {
	for _, kind := range []string{"escape-after-symlink", "cycle", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			s, b, source := fixture(t)
			external := filepath.Join(filepath.Dir(source.ProjectsPath), "seed.json")
			os.WriteFile(external, []byte("private-canary"), 0600)
			switch kind {
			case "escape-after-symlink":
				os.Symlink(".", filepath.Join(source.ProjectsPath, "same"))
				os.Symlink("same/../seed.json", filepath.Join(source.ProjectsPath, "escape"))
			case "cycle":
				os.Symlink("b", filepath.Join(source.ProjectsPath, "a"))
				os.Symlink("a", filepath.Join(source.ProjectsPath, "b"))
			case "dangling":
				os.Symlink("missing", filepath.Join(source.ProjectsPath, "dangling"))
			}
			_, e := s.Seed(context.Background(), b, source, allow)
			assertCode(t, e, "invalid_seed")
			if _, e = os.Lstat(filepath.Join(s.Root, b.StorageID)); !os.IsNotExist(e) {
				t.Fatal("invalid link published")
			}
			raw, e := os.ReadFile(external)
			if e != nil || string(raw) != "private-canary" {
				t.Fatal("external data changed")
			}
		})
	}
}

func TestSeedFullyResolvedContainedLinkChainKeepsOriginalLinkBytes(t *testing.T) {
	s, b, source := fixture(t)
	os.Symlink("dir/link", filepath.Join(source.ProjectsPath, "chain"))
	h, e := s.Seed(context.Background(), b, source, allow)
	if e != nil {
		t.Fatal(e)
	}
	link, e := os.Readlink(filepath.Join(h.Data, "chain"))
	if e != nil || link != "dir/link" {
		t.Fatal("link rewritten", link, e)
	}
	raw, e := os.ReadFile(filepath.Join(h.Data, "chain"))
	if e != nil || string(raw) != string([]byte{0, 1, 255, 128}) {
		t.Fatal("contained chain broken", e)
	}
}
