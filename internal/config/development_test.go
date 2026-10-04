package config

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDevelopmentPrefixValue(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string // want is a part of the refusal, "" accepts
	}{
		{"absolute and clean", "/Users/dev/.local", ""},
		{"relative", ".local", "absolute"},
		{"unclean", "/Users/dev/../dev/.local", "clean"},
		{"newline", "/Users/dev/.local\nwhr setup", "control"},
		{"tab", "/Users/dev/.local\tx", "control"},
		{"bidi override", "/Users/dev/\u202e.local", "bidirectional"},
		{"line separator", "/Users/dev/\u2028.local", "separator"},
		{"admin prefix", "/opt/whr", "managed prefix"},
		{"homebrew", "/opt/homebrew", "managed prefix"},
		{"usr local", "/usr/local", "managed prefix"},
		{"homebrew formula", "/opt/homebrew/opt/whr", "managed prefix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckDevelopmentPrefix(tc.value)
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("CheckDevelopmentPrefix(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// A managed prefix reached by a link is refused by identity, not by spelling.
func TestDevelopmentPrefixManagedByIdentity(t *testing.T) {
	if _, err := os.Stat("/usr/local"); err != nil {
		t.Skip("no /usr/local here")
	}
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink("/usr/local", link); err != nil {
		t.Fatal(err)
	}
	if got := CheckDevelopmentPrefix(link); !strings.Contains(got, "managed prefix") {
		t.Errorf("a link to /usr/local = %q, want the managed-prefix refusal", got)
	}
}

type fakeInfo struct {
	mode fs.FileMode
	sys  any
}

func (f fakeInfo) Name() string       { return "config.json" }
func (f fakeInfo) Size() int64        { return 1 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return f.sys }

func TestDevelopmentFileChecks(t *testing.T) {
	const uid = 501
	own := &syscall.Stat_t{Uid: uid, Nlink: 1}
	for _, tc := range []struct {
		name   string
		info   fakeInfo
		linked bool
		want   string // "" accepts
	}{
		{"own file", fakeInfo{0o600, own}, false, ""},
		{"own file 0644", fakeInfo{0o644, own}, false, ""},
		{"root's file", fakeInfo{0o644, &syscall.Stat_t{Uid: 0, Nlink: 1}}, false, ""},
		{"group writable", fakeInfo{0o620, own}, false, "group or other"},
		{"other writable", fakeInfo{0o602, own}, false, "group or other"},
		{"another account", fakeInfo{0o600, &syscall.Stat_t{Uid: 502, Nlink: 1}}, false, "another account"},
		{"two links", fakeInfo{0o600, &syscall.Stat_t{Uid: uid, Nlink: 2}}, false, "hard links"},
		{"a directory", fakeInfo{fs.ModeDir | 0o700, own}, false, "not a regular file"},
		{"a link", fakeInfo{0o600, own}, true, "symbolic link"},
		{"owner unknown", fakeInfo{0o600, nil}, false, "cannot be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(checkDevelopmentFile(filepath.Join(t.TempDir(), "config.json"), tc.info, tc.linked, uid, nil), "\n")
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("problems = %q, want %q", got, tc.want)
			}
		})
	}
}

// writeDev writes the rig's configuration with the key set and returns the path.
func (r *rig) writeDev(t *testing.T, name, prefix string, mode os.FileMode) string {
	t.Helper()
	r.cfg.DevelopmentPrefix = prefix
	raw, err := json.MarshalIndent(r.cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.dir, name)
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // the umask must not decide
		t.Fatal(err)
	}
	return path
}

func TestLoadChecksTheFileOfTheKeyOnTheDescriptor(t *testing.T) {
	r := newRig(t)
	prefix := filepath.Join(r.dir, "dev")

	good := r.writeDev(t, "good.json", prefix, 0o600)
	if c, err := Load(good); err != nil || c.DevelopmentPrefix != prefix {
		t.Fatalf("Load(good) = %v, %v", c, err)
	}
	if got, err := ReadDevelopmentPrefix(good); err != nil || got != prefix {
		t.Fatalf("ReadDevelopmentPrefix(good) = %q, %v", got, err)
	}

	cases := []struct {
		name string
		path func(t *testing.T) string
		want string
	}{
		{"symbolic link", func(t *testing.T) string {
			p := r.writeDev(t, "target.json", prefix, 0o600)
			l := filepath.Join(r.dir, "viaLink.json")
			if err := os.Symlink(p, l); err != nil {
				t.Fatal(err)
			}
			return l
		}, "symbolic link"},
		{"hard link", func(t *testing.T) string {
			p := r.writeDev(t, "orig.json", prefix, 0o600)
			if err := os.Link(p, filepath.Join(r.dir, "second.json")); err != nil {
				t.Fatal(err)
			}
			return p
		}, "hard links"},
		{"group writable", func(t *testing.T) string { return r.writeDev(t, "gw.json", prefix, 0o660) }, "group or other"},
		{"other writable", func(t *testing.T) string { return r.writeDev(t, "ow.json", prefix, 0o606) }, "group or other"},
		{"managed prefix", func(t *testing.T) string { return r.writeDev(t, "m.json", "/opt/whr", 0o600) }, "managed prefix"},
		{"relative", func(t *testing.T) string { return r.writeDev(t, "rel.json", "dev", 0o600) }, "absolute"},
		{"in a workspace root", func(t *testing.T) string {
			return r.writeDev(t, filepath.Join("workspaces", "c.json"), prefix, 0o600)
		}, "workspace root"},
		{"in a git working tree", func(t *testing.T) string {
			repo := filepath.Join(r.dir, "repo")
			if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
				t.Fatal(err)
			}
			return r.writeDev(t, filepath.Join("repo", "c.json"), prefix, 0o600)
		}, "git working tree"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path(t)
			_, err := Load(path)
			if got := problems(err); !strings.Contains(got, tc.want) {
				t.Errorf("Load = %q, want %q", got, tc.want)
			}
			got, err := ReadDevelopmentPrefix(path)
			if got != "" || !strings.Contains(problems(err), tc.want) {
				t.Errorf("ReadDevelopmentPrefix = %q, %q, want a refusal with %q", got, problems(err), tc.want)
			}
		})
	}
}

// The tightening applies only while the key is set: a linked, group-writable
// configuration without it loads as before.
func TestTheFileChecksNeedTheKey(t *testing.T) {
	r := newRig(t)
	path := r.writeDev(t, "plain.json", "", 0o660)
	link := filepath.Join(r.dir, "plainlink.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err != nil {
		t.Errorf("Load without the key = %v", err)
	}
	if got, err := ReadDevelopmentPrefix(link); got != "" || err != nil {
		t.Errorf("ReadDevelopmentPrefix without the key = %q, %v", got, err)
	}
}

func TestReadDevelopmentPrefixCases(t *testing.T) {
	r := newRig(t)
	write := func(name, body string) string {
		p := filepath.Join(r.dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, tc := range []struct {
		name, path, got, err string
	}{
		{"no file", filepath.Join(r.dir, "none.json"), "", ""},
		{"not JSON", write("bad.json", "{"), "", ""},
		{"no key", write("nokey.json", `{"listen":"127.0.0.1:1"}`), "", ""},
		{"empty value is no key", write("empty.json", `{"development_prefix":""}`), "", ""},
		{"wrong type", write("num.json", `{"development_prefix":5}`), "", "wrong type"},
		{"a value", write("ok.json", `{"development_prefix":"`+filepath.Join(r.dir, "dev")+`"}`), filepath.Join(r.dir, "dev"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadDevelopmentPrefix(tc.path)
			if got != tc.got || (tc.err == "") != (err == nil) || !strings.Contains(problems(err), tc.err) {
				t.Errorf("= %q, %v; want %q, error containing %q", got, err, tc.got, tc.err)
			}
		})
	}
}

// No environment variable sets, clears or changes the key.
func TestNoEnvironmentVariableTouchesTheKey(t *testing.T) {
	r := newRig(t)
	with := r.writeDev(t, "with.json", filepath.Join(r.dir, "dev"), 0o600)
	r.cfg.DevelopmentPrefix = ""
	without := r.writeDev(t, "without.json", "", 0o600)
	for _, k := range []string{"WORKHARBOR_DEV", "WHR_DEV", "WHR_DEVELOPMENT_PREFIX", "WORKHARBOR_DEVELOPMENT_PREFIX", "DEVELOPMENT_PREFIX"} {
		t.Setenv(k, "1")
		t.Setenv(k, filepath.Join(r.dir, "other"))
	}
	if got, err := ReadDevelopmentPrefix(without); got != "" || err != nil {
		t.Errorf("without the key = %q, %v: an environment variable turned the mode on", got, err)
	}
	if got, err := ReadDevelopmentPrefix(with); got != filepath.Join(r.dir, "dev") || err != nil {
		t.Errorf("with the key = %q, %v: an environment variable changed it", got, err)
	}
	if c, err := Load(without); err != nil || c.DevelopmentPrefix != "" {
		t.Errorf("Load without the key = %+v, %v", c, err)
	}
}

func TestUnderManagedPrefix(t *testing.T) {
	dir := t.TempDir()
	if UnderManagedPrefix(filepath.Join(dir, "bin", "whr")) {
		t.Error("a temporary directory is no managed prefix")
	}
	if _, err := os.Stat("/usr/local"); err == nil && !UnderManagedPrefix("/usr/local/bin/whr") {
		t.Error("/usr/local/bin/whr lies in a managed prefix")
	}
	if !UnderManagedPrefix("/opt/whr/bin/whr") && statOK("/opt/whr") {
		t.Error("/opt/whr/bin/whr lies in a managed prefix")
	}
}

func statOK(p string) bool { _, err := os.Stat(p); return err == nil }
