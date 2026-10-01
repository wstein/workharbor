package runtime

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeFS is an in-memory filesystem. Lookups ignore case, like a default APFS
// volume, and links are followed the way EvalSymlinks does.
type fakeFS struct {
	links map[string]string      // link path -> absolute target
	nodes map[string]fs.FileMode // existing paths (lower-cased)
}

func newFakeFS() *fakeFS {
	return &fakeFS{links: map[string]string{}, nodes: map[string]fs.FileMode{"/": fs.ModeDir}}
}

// add creates path with mode, and every directory above it.
func (f *fakeFS) add(path string, mode fs.FileMode) {
	for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, ok := f.nodes[strings.ToLower(dir)]; !ok {
			f.nodes[strings.ToLower(dir)] = fs.ModeDir
		}
	}
	f.nodes[strings.ToLower(path)] = mode
}

func (f *fakeFS) dir(path string)    { f.add(path, fs.ModeDir) }
func (f *fakeFS) file(path string)   { f.add(path, 0o644) }
func (f *fakeFS) socket(path string) { f.add(path, fs.ModeSocket|0o600) }

func (f *fakeFS) link(path, target string) {
	f.add(path, fs.ModeSymlink)
	f.links[strings.ToLower(path)] = target
}

func (f *fakeFS) EvalSymlinks(path string) (string, error) {
	path = filepath.Clean(path)
	for range 32 {
		followed := false
		// Longest link first, so nested links resolve from the leaf up.
		keys := make([]string, 0, len(f.links))
		for k := range f.links {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
		for _, k := range keys {
			lower := strings.ToLower(path)
			if lower == k || strings.HasPrefix(lower, k+"/") {
				path = filepath.Clean(f.links[k] + path[len(k):])
				followed = true
				break
			}
		}
		if !followed {
			if _, ok := f.nodes[strings.ToLower(path)]; !ok {
				return "", &fs.PathError{Op: "lstat", Path: path, Err: fs.ErrNotExist}
			}
			return path, nil
		}
	}
	return "", &fs.PathError{Op: "evalsymlinks", Path: path, Err: errors.New("too many links")}
}

func (f *fakeFS) Stat(path string) (fs.FileInfo, error) {
	resolved, err := f.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	return fakeInfo{name: filepath.Base(resolved), mode: f.nodes[strings.ToLower(resolved)]}, nil
}

type fakeInfo struct {
	name string
	mode fs.FileMode
}

func (i fakeInfo) Name() string       { return i.name }
func (i fakeInfo) Size() int64        { return 0 }
func (i fakeInfo) Mode() fs.FileMode  { return i.mode }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i fakeInfo) Sys() any           { return nil }

const testHome = "/Users/me"

func testFS() *fakeFS {
	f := newFakeFS()
	// A macOS-like layout: /var is a link to /private/var.
	f.dir("/private/var/run")
	f.dir("/private/var/folders/xx/T/proj")
	f.link("/var", "/private/var")
	f.dir("/Users/me/src/app")
	f.file("/Users/me/src/app/main.go")
	f.dir("/Users/me/.ssh")
	f.file("/Users/me/.ssh/id_ed25519")
	f.dir("/Users/me/.config/gh")
	f.dir("/Users/me/.config/other")
	f.dir("/Users/me/.socktainer")
	f.dir("/Users/me/.claude")
	f.dir("/Users/me/Library/Keychains")
	f.dir("/Users/me/Library/Preferences")
	f.dir("/Users/other/src")
	f.dir("/run/user/1000")
	f.dir("/etc/ssh")
	f.dir("/System/Library")
	f.dir("/tmp/socks")
	f.dir("/Volumes")
	f.dir("/Library")
	f.dir("/home")
	f.socket("/tmp/agent.sock")
	f.socket("/private/var/run/docker.sock")
	f.link("/work/home", "/Users/me")
	f.link("/work/ssh", "/Users/me/.ssh")
	f.link("/work/app", "/Users/me/src/app")
	f.link("/work/loop-a", "/work/loop-b")
	f.link("/work/loop-b", "/work/loop-a")
	return f
}

func TestCheckMount(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   Reason // empty means the mount is allowed
	}{
		{"project directory", "/Users/me/src/app", ""},
		{"another user's project is outside our concern", "/Users/other/src", ""},
		{"temp directory below /private/var", "/private/var/folders/xx/T/proj", ""},
		{"temp directory through the /var link", "/var/folders/xx/T/proj", ""},
		{"link to a project", "/work/app", ""},
		{"directory below home that is not a secret", "/Users/me/.config/other", ""},
		{"directory below Library that is not Keychains", "/Users/me/Library/Preferences", ""},
		{"a directory that merely contains sockets", "/tmp/socks", ""},

		{"home", "/Users/me", ReasonHome},
		{"home with a trailing slash", "/Users/me/", ReasonHome},
		{"home through a .. segment", "/Users/me/src/..", ReasonHome},
		{"home through a link", "/work/home", ReasonHome},
		{"home in other case", "/USERS/Me", ReasonHome},
		{"home parent", "/Users", ReasonHomeParent},
		{"root", "/", ReasonHomeParent},

		{"ssh directory", "/Users/me/.ssh", ReasonSecrets},
		{"ssh directory in other case", "/users/me/.SSH", ReasonSecrets},
		{"a key file", "/Users/me/.ssh/id_ed25519", ReasonSecrets},
		{"ssh through a .. segment", "/Users/me/src/../.ssh", ReasonSecrets},
		{"ssh through a link", "/work/ssh", ReasonSecrets},
		{"a directory that contains a secret", "/Users/me/.config", ReasonSecrets},
		{"github cli credentials", "/Users/me/.config/gh", ReasonSecrets},
		{"agent login", "/Users/me/.claude", ReasonSecrets},
		{"keychains", "/Users/me/Library/Keychains", ReasonSecrets},
		{"Library, which contains the keychains", "/Users/me/Library", ReasonSecrets},

		{"socktainer directory", "/Users/me/.socktainer", ReasonRuntimeSocket},
		{"runtime socket directory", "/private/var/run", ReasonRuntimeSocket},
		{"runtime socket directory through the /var link", "/var/run", ReasonRuntimeSocket},
		{"run", "/run", ReasonRuntimeSocket},
		{"below run", "/run/user/1000", ReasonRuntimeSocket},
		{"a directory that contains the runtime socket directory", "/private/var", ReasonSystem},

		{"unix socket", "/tmp/agent.sock", ReasonSocket},
		{"runtime unix socket", "/private/var/run/docker.sock", ReasonSocket},

		{"/etc", "/etc", ReasonSystem},
		{"below /etc", "/etc/ssh", ReasonSystem},
		{"/System", "/System/Library", ReasonSystem},
		{"/var", "/var", ReasonSystem},
		{"/Volumes", "/Volumes", ReasonSystem},
		{"/Library", "/Library", ReasonSystem},
		{"/home", "/home", ReasonSystem},

		{"relative path", "src/app", ReasonNotAbsolute},
		{"empty path", "", ReasonNotAbsolute},
		{"missing path", "/Users/me/src/missing", ReasonUnresolvable},
		{"link loop", "/work/loop-a", ReasonUnresolvable},
	}
	fsys := testFS()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckMount(fsys, testHome, tc.source)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckMount(%q) = %v, want it allowed", tc.source, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckMount(%q) allowed, want %q", tc.source, tc.want)
			}
			if !errors.Is(err, ErrForbiddenMount) {
				t.Errorf("errors.Is(%v, ErrForbiddenMount) = false", err)
			}
			var me *MountError
			if !errors.As(err, &me) {
				t.Fatalf("error %T is not a *MountError", err)
			}
			if me.Reason != tc.want {
				t.Errorf("CheckMount(%q) reason = %q, want %q (%v)", tc.source, me.Reason, tc.want, err)
			}
		})
	}
}

func TestCheckMountHomeIsResolvedToo(t *testing.T) {
	// The home directory itself may be reached through a link.
	f := testFS()
	f.link("/export/me", "/Users/me")
	if err := CheckMount(f, "/export/me", "/Users/me/.ssh"); err == nil {
		t.Fatal("secrets must be found when HOME is a symbolic link")
	}
	if err := CheckMount(f, "/export/me", "/Users/me/src/app"); err != nil {
		t.Fatalf("project below a linked HOME was rejected: %v", err)
	}
}

func TestCheckMountFailsClosedWithoutHome(t *testing.T) {
	err := CheckMount(testFS(), "", "/Users/me/src/app")
	if err == nil {
		t.Fatal("an unknown home directory must not allow a mount")
	}
	if errors.Is(err, ErrForbiddenMount) {
		t.Errorf("%v: a missing home is a caller error, not a forbidden mount", err)
	}
	if err := CheckMount(testFS(), "/Users/nobody", "/Users/me/src/app"); err == nil {
		t.Fatal("an unresolvable home directory must not allow a mount")
	}
}

func TestMountErrorMessageNamesTheTarget(t *testing.T) {
	err := CheckMount(testFS(), testHome, "/work/ssh")
	for _, want := range []string{"/work/ssh", "/Users/me/.ssh", string(ReasonSecrets)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestCheckMountsReportsEveryRejection(t *testing.T) {
	mounts := []Mount{
		{Source: "/Users/me/src/app", Target: "/work"},
		{Source: "/Users/me/.ssh", Target: "/root/.ssh", ReadOnly: true},
		{Source: "/etc", Target: "/etc", ReadOnly: true},
	}
	err := CheckMounts(testFS(), testHome, mounts)
	if err == nil {
		t.Fatal("expected the forbidden mounts to be reported")
	}
	for _, want := range []string{"/Users/me/.ssh", "/etc"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "/Users/me/src/app") {
		t.Errorf("error %q mentions the allowed mount", err)
	}
	if err := CheckMounts(testFS(), testHome, mounts[:1]); err != nil {
		t.Errorf("allowed mounts were rejected: %v", err)
	}
	if err := CheckMounts(testFS(), testHome, nil); err != nil {
		t.Errorf("no mounts were rejected: %v", err)
	}
}

func TestReadOnlyDoesNotMakeASecretMountable(t *testing.T) {
	m := Mount{Source: "/Users/me/.ssh", Target: "/root/.ssh", ReadOnly: true}
	if err := CheckMounts(testFS(), testHome, []Mount{m}); err == nil {
		t.Fatal("a read-only mount of ~/.ssh must be rejected")
	}
}
