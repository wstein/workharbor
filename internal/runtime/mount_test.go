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
	links   map[string]string      // link path -> absolute target
	nodes   map[string]fs.FileMode // existing paths (lower-cased)
	aliases map[string]string      // lower-cased path -> the path it is the same file as
}

func newFakeFS() *fakeFS {
	return &fakeFS{links: map[string]string{}, nodes: map[string]fs.FileMode{"/": fs.ModeDir}, aliases: map[string]string{}}
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

// alias makes two paths the same file, as a precomposed and a decomposed
// spelling of one name are on APFS.
func (f *fakeFS) alias(a, b string, mode fs.FileMode) {
	f.add(a, mode)
	f.add(b, mode)
	f.aliases[strings.ToLower(b)] = strings.ToLower(a)
}

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
	id := strings.ToLower(resolved)
	if canonical, ok := f.aliases[id]; ok {
		id = canonical
	}
	return fakeInfo{name: filepath.Base(resolved), mode: f.nodes[strings.ToLower(resolved)], id: id}, nil
}

func (f *fakeFS) SameFile(a, b fs.FileInfo) bool {
	ia, okA := a.(fakeInfo)
	ib, okB := b.(fakeInfo)
	return okA && okB && ia.id == ib.id
}

type fakeInfo struct {
	name string
	mode fs.FileMode
	id   string // equal for two paths that are the same file
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
	f.dir("/Users/me/Library/Group Containers/2BUA8C4S2C.com.1password")
	f.dir("/Users/me/Library/Application Support/Google/Chrome")
	f.dir("/Users/me/.orbstack/run")
	f.dir("/Users/me/.colima/default")
	f.dir("/Users/me/.lima/default")
	f.dir("/Users/me/.local/share/containers/storage")
	f.dir("/Users/me/.local/share/other")
	f.dir("/Users/me/.password-store")
	f.dir("/private/tmp/proj")
	f.dir("/private/var/root/.ssh")
	f.dir("/Users/Shared")
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

// dotfilesFS is a home whose secrets are symbolic links into a dotfiles
// directory, as stow and chezmoi leave them.
func dotfilesFS() *fakeFS {
	f := newFakeFS()
	f.dir("/Users/me/src/app")
	f.dir("/Users/me/dotfiles/ssh")
	f.file("/Users/me/dotfiles/ssh/id_ed25519")
	f.dir("/Users/me/dotfiles/gh")
	f.file("/Users/me/dotfiles/gh/hosts.yml")
	f.dir("/Users/me/dotfiles/vim")
	f.dir("/Users/me/.config")
	f.link("/Users/me/.ssh", "/Users/me/dotfiles/ssh")
	f.link("/Users/me/.config/gh", "/Users/me/dotfiles/gh")
	return f
}

// #50: secrets paths were never resolved, so mounting the directory they link
// into passed.
func TestCheckMountResolvesSymlinkedSecrets(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   Reason
	}{
		{"the dotfiles directory that holds the secrets", "/Users/me/dotfiles", ReasonSecrets},
		{"the linked ssh directory", "/Users/me/dotfiles/ssh", ReasonSecrets},
		{"a key inside it", "/Users/me/dotfiles/ssh/id_ed25519", ReasonSecrets},
		{"the linked gh directory", "/Users/me/dotfiles/gh", ReasonSecrets},
		{"a file inside it", "/Users/me/dotfiles/gh/hosts.yml", ReasonSecrets},
		{"the link itself", "/Users/me/.ssh", ReasonSecrets},
		{"a directory containing the gh link", "/Users/me/.config", ReasonSecrets},

		{"a dotfiles directory without secrets", "/Users/me/dotfiles/vim", ""},
		{"a project", "/Users/me/src/app", ""},
	}
	fsys := dotfilesFS()
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
			if got := reasonOfErr(t, err); got != tc.want {
				t.Errorf("CheckMount(%q) reason = %q, want %q (%v)", tc.source, got, tc.want, err)
			}
		})
	}
}

func reasonOfErr(t *testing.T, err error) Reason {
	t.Helper()
	var me *MountError
	if !errors.As(err, &me) {
		t.Fatalf("error %v is not a *MountError", err)
	}
	return me.Reason
}

// #50: locations that passed before.
func TestCheckMountRejectsMoreSecretsAndRuntimeLocations(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   Reason
	}{
		{"1password agent socket folder", "/Users/me/Library/Group Containers", ReasonSecrets},
		{"inside it", "/Users/me/Library/Group Containers/2BUA8C4S2C.com.1password", ReasonSecrets},
		{"browser cookies", "/Users/me/Library/Application Support", ReasonSecrets},
		{"a browser profile", "/Users/me/Library/Application Support/Google/Chrome", ReasonSecrets},
		{"password store", "/Users/me/.password-store", ReasonSecrets},

		{"orbstack", "/Users/me/.orbstack", ReasonRuntimeSocket},
		{"below orbstack", "/Users/me/.orbstack/run", ReasonRuntimeSocket},
		{"colima", "/Users/me/.colima", ReasonRuntimeSocket},
		{"lima", "/Users/me/.lima/default", ReasonRuntimeSocket},
		{"podman storage", "/Users/me/.local/share/containers", ReasonRuntimeSocket},
		{"below podman storage", "/Users/me/.local/share/containers/storage", ReasonRuntimeSocket},

		{"/tmp", "/tmp", ReasonSystem},
		{"/private/tmp", "/private/tmp", ReasonSystem},
		{"user temp base", "/private/var/folders", ReasonSystem},
		{"root's home on macOS", "/private/var/root", ReasonSystem},
		{"below root's home", "/private/var/root/.ssh", ReasonSystem},
		{"shared users folder", "/Users/Shared", ReasonSystem},

		{"a directory below /private/tmp", "/private/tmp/proj", ""},
		{"a sibling of podman storage", "/Users/me/.local/share/other", ""},
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
			if got := reasonOfErr(t, err); got != tc.want {
				t.Errorf("CheckMount(%q) reason = %q, want %q (%v)", tc.source, got, tc.want, err)
			}
		})
	}
}

// #50: a home stored decomposed (NFD) compared unequal to the same path
// written precomposed, so the home and ~/.ssh passed.
func TestCheckMountComparesByFileIdentity(t *testing.T) {
	const (
		homeNFD = "/Users/ju\u0308rgen"
		homeNFC = "/Users/j\u00fcrgen"
	)
	f := newFakeFS()
	f.alias(homeNFD, homeNFC, fs.ModeDir)
	f.alias(homeNFD+"/.ssh", homeNFC+"/.ssh", fs.ModeDir)
	f.alias(homeNFD+"/src", homeNFC+"/src", fs.ModeDir)

	tests := []struct {
		name   string
		source string
		want   Reason
	}{
		{"the home as it is stored", homeNFD, ReasonHome},
		{"the home precomposed", homeNFC, ReasonHome},
		{"ssh precomposed", homeNFC + "/.ssh", ReasonSecrets},
		{"ssh decomposed", homeNFD + "/.ssh", ReasonSecrets},
		{"a project either way", homeNFC + "/src", ""},
		{"a project, decomposed", homeNFD + "/src", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckMount(f, homeNFD, tc.source)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("CheckMount(%q) = %v, want it allowed", tc.source, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckMount(%q) allowed, want %q", tc.source, tc.want)
			}
			if got := reasonOfErr(t, err); got != tc.want {
				t.Errorf("CheckMount(%q) reason = %q, want %q (%v)", tc.source, got, tc.want, err)
			}
		})
	}
}
