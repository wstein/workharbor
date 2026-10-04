package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wstein/workharbor/internal/textsafe"
)

// DevelopmentPrefixKey is the top-level key that remembers a development
// installation (D24, issue #276). Its value is the absolute prefix. It is
// written only by an explicit `whr setup --dev` and removed by `whr setup
// --managed` or by hand; no environment variable, default, `whr doctor`, `whr
// serve`, `whr service` or file of a workspace or repository sets it. Anything
// running as the account can write the file too, so the key is no protection:
// it makes the weaker mode visible, and a managed installation refuses it.
const DevelopmentPrefixKey = "development_prefix"

// managedPrefixes are the admin-owned places a whr is installed (D24): the
// admin prefix and Homebrew's. A remembered development prefix is never one,
// and a whr run from one of them refuses the key.
var managedPrefixes = []string{"/opt/whr", "/opt/homebrew", "/usr/local", "/opt/homebrew/opt/whr", "/opt/homebrew/Cellar/whr", "/usr/local/opt/whr"}

// isManagedPrefix reports whether p is a managed prefix, by spelling and, where
// the managed prefix exists, by identity (a link or a case variant on a
// case-insensitive disk does not hide it).
func isManagedPrefix(p string) bool {
	pi, perr := os.Stat(p)
	for _, m := range managedPrefixes {
		if filepath.Clean(p) == m {
			return true
		}
		if mi, err := os.Stat(m); err == nil && perr == nil && os.SameFile(pi, mi) {
			return true
		}
	}
	return false
}

// UnderManagedPrefix reports whether path (a whr binary) lies in a managed
// prefix, compared by identity. A managed installation refuses the key.
func UnderManagedPrefix(path string) bool {
	for _, m := range managedPrefixes {
		if within(path, m) {
			return true
		}
	}
	return false
}

// CheckDevelopmentPrefix applies to the key's value every check the --prefix
// flag has: absolute, clean, no control, bidirectional or separator character,
// and not a managed prefix. It returns "" when the value passes. The checks
// against the file system (owner, writers, the home) run where the prefix is
// used, on every read (`whr doctor`).
func CheckDevelopmentPrefix(v string) string {
	switch {
	case strings.IndexFunc(v, func(r rune) bool { return textsafe.IsControl(r) || textsafe.IsBidiOrSeparator(r) || r == '\t' }) >= 0:
		return "must not contain a control, bidirectional or separator character"
	case !filepath.IsAbs(v) || filepath.Clean(v) != v:
		return "must be an absolute, clean path"
	case isManagedPrefix(v):
		return "is a managed prefix (a managed installation takes no development_prefix): run `whr setup --managed`"
	}
	return ""
}

// configFile is a configuration file opened once: its content and the
// description of the open descriptor, never of the path.
type configFile struct {
	raw    []byte
	info   os.FileInfo
	linked bool // the path itself is a symbolic link
}

// openConfig opens path with O_NOFOLLOW and reads the descriptor, as ReadSecret
// does, so what is checked is the file that was read. A path that is a link
// (ELOOP) is opened through it and marked, because the key is refused through
// a link while the file stays usable without the key.
func openConfig(path string) (configFile, error) {
	const flags = os.O_RDONLY | syscall.O_NONBLOCK
	linked := false
	f, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW, 0) //nolint:gosec // the operator names the config file; checked on the open file
	if errors.Is(err, syscall.ELOOP) {
		linked = true
		f, err = os.OpenFile(path, flags, 0) //nolint:gosec // as above, marked as reached through a link
	}
	if err != nil {
		return configFile{}, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return configFile{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return configFile{}, err
	}
	return configFile{raw: raw, info: info, linked: linked}, nil
}

// checkDevelopmentFile lists what is wrong with the file that holds the key:
// a regular file with one link, owned by uid or root, closed to group and other
// writers, not reached through a link, outside every workspace root and git
// working tree. The workspace roots come from the same file, so that check
// catches a stray file, not a crafted one: the control is that a managed
// installation refuses the key. This tightening applies only when the key is set.
func checkDevelopmentFile(path string, info os.FileInfo, linked bool, uid int, roots []string) []string {
	var out []string
	add := func(format string, args ...any) {
		out = append(out, DevelopmentPrefixKey+": "+fmt.Sprintf(format, args...))
	}
	if linked {
		add("%s is a symbolic link; the file that holds the key must be the file itself", path)
	}
	if !info.Mode().IsRegular() {
		add("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		add("%s can be written by group or other (mode %04o)", path, info.Mode().Perm())
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if int(st.Uid) != uid && st.Uid != 0 {
			add("%s belongs to another account than the one running whr, or root", path)
		}
		if st.Nlink != 1 {
			add("%s has %d hard links, it must have one", path, st.Nlink)
		}
	} else {
		add("%s: owner and links cannot be read", path)
	}
	resolved := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		resolved = r
	}
	for _, root := range roots {
		if within(resolved, root) {
			add("%s is inside the workspace root %s, where an agent writes", path, root)
		}
	}
	if repo := enclosingRepo(filepath.Dir(resolved)); repo != "" {
		add("%s is inside the git working tree %s", path, repo)
	}
	return out
}

// ReadDevelopmentPrefix returns the remembered development prefix of the
// configuration at path, or "" when there is none (no file, no key, or a file
// that is not JSON, which the configuration check reports). It looks at the
// key alone, so setup and doctor can use it while the rest of the file is
// unfinished. When the key is set, its value and the file are checked as Load
// checks them, and a failure is an *Error: the key is refused, never trusted.
func ReadDevelopmentPrefix(path string) (string, error) {
	cf, err := openConfig(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var v struct {
		DevelopmentPrefix string `json:"development_prefix"`
		Roots             struct {
			Workspaces []string `json:"workspaces"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(cf.raw, &v); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && (te.Field == DevelopmentPrefixKey || strings.HasPrefix(te.Field, "roots")) {
			return "", &Error{Problems: []string{DevelopmentPrefixKey + ": the key or the workspace roots have the wrong type"}}
		}
		return "", nil
	}
	if v.DevelopmentPrefix == "" {
		return "", nil
	}
	problems := developmentProblems(path, cf, v.DevelopmentPrefix, v.Roots.Workspaces)
	if len(problems) > 0 {
		return "", &Error{Problems: problems}
	}
	return v.DevelopmentPrefix, nil
}

func developmentProblems(path string, cf configFile, prefix string, roots []string) []string {
	var problems []string
	if msg := CheckDevelopmentPrefix(prefix); msg != "" {
		problems = append(problems, DevelopmentPrefixKey+": "+msg)
	}
	return append(problems, checkDevelopmentFile(path, cf.info, cf.linked, os.Getuid(), roots)...) //nolint:gosec // a uid fits an int
}
