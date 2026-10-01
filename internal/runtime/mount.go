package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// ErrForbiddenMount is matched by every error CheckMount returns for a source
// that must not be bind-mounted into an environment.
var ErrForbiddenMount = errors.New("forbidden mount")

// Reason says why a mount source was rejected.
type Reason string

const (
	ReasonNotAbsolute   Reason = "not an absolute path"
	ReasonUnresolvable  Reason = "cannot be resolved"
	ReasonSocket        Reason = "unix socket"
	ReasonHome          Reason = "home directory"
	ReasonHomeParent    Reason = "parent of the home directory"
	ReasonSecrets       Reason = "secrets directory"
	ReasonRuntimeSocket Reason = "runtime socket directory"
	ReasonSystem        Reason = "system directory"
)

// MountError describes a rejected mount source.
type MountError struct {
	Source   string // as given
	Resolved string // after symlinks; empty if it could not be resolved
	Reason   Reason
	Err      error // the underlying error, for ReasonUnresolvable
}

func (e *MountError) Error() string {
	msg := fmt.Sprintf("mount %q rejected: %s", e.Source, e.Reason)
	if e.Resolved != "" && e.Resolved != e.Source {
		msg += fmt.Sprintf(" (resolves to %q)", e.Resolved)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *MountError) Unwrap() error { return e.Err }

// Is makes errors.Is(err, ErrForbiddenMount) true for every MountError.
func (e *MountError) Is(target error) bool { return target == ErrForbiddenMount }

// FS is the part of the filesystem CheckMount reads, so tests can supply a
// fake one.
type FS interface {
	// EvalSymlinks returns the path after resolving every symbolic link.
	EvalSymlinks(path string) (string, error)
	// Stat returns file information, following symbolic links.
	Stat(path string) (fs.FileInfo, error)
	// SameFile reports whether two FileInfo values describe the same file.
	SameFile(a, b fs.FileInfo) bool
}

// secretsUnderHome are directories and files under the home directory that
// hold credentials, relative to it. An agent environment never gets them,
// nor a directory that contains them.
var secretsUnderHome = []string{
	".ssh",
	".gnupg",
	".aws",
	".azure",
	".kube",
	".docker",
	".config/gh",
	".config/gcloud",
	".netrc",
	".git-credentials",
	".npmrc",
	".pypirc",
	".password-store",
	".claude",
	".codex",
	"Library/Keychains",
	"Library/Group Containers",    // holds the 1Password agent socket
	"Library/Application Support", // browser cookies and other apps' stored logins
}

// runtimeSocketDirs are directories where container runtimes and daemons keep
// their sockets, relative to the home directory or absolute.
var (
	runtimeSocketDirsUnderHome = []string{".socktainer", ".docker/run", ".orbstack", ".colima", ".lima", ".local/share/containers"}
	runtimeSocketDirs          = []string{"/var/run", "/private/var/run", "/run"}
)

// systemRoots are directories that are rejected exactly, because they hold
// every user or every volume; systemTrees are rejected with everything below.
var (
	systemRoots = []string{"/", "/Users", "/Users/Shared", "/home", "/private", "/var", "/private/var", "/private/var/folders", "/tmp", "/private/tmp", "/Volumes", "/Library"}
	systemTrees = []string{"/etc", "/private/etc", "/System", "/dev", "/proc", "/sys", "/boot", "/root", "/private/var/root"}
)

// CheckMount reports whether source may be bind-mounted into an agent
// environment. The runtime accepts any host path, so the adapter calls this
// before it asks the runtime to mount (design §4.4, §7.4).
//
// It resolves symbolic links first, so a link to the home directory is judged
// by where it leads. It then rejects, whether the mount is read-only or not:
//
//   - a path that is not absolute or cannot be resolved,
//   - a unix socket,
//   - the home directory and any of its parents,
//   - the secrets directories under the home directory, and any directory
//     that contains one (such as ~/.config),
//   - the directories where container runtimes keep their sockets,
//   - system roots such as / and /Users, and system trees such as /etc.
//
// Paths are compared by file identity, not by string, so case variants and
// Unicode normalization forms of one name are the same path (see identities).
// home is the user's home directory.
func CheckMount(fsys FS, home, source string) error {
	if !filepath.IsAbs(source) {
		return &MountError{Source: source, Reason: ReasonNotAbsolute}
	}
	resolved, err := fsys.EvalSymlinks(filepath.Clean(source))
	if err != nil {
		return &MountError{Source: source, Reason: ReasonUnresolvable, Err: err}
	}
	reject := func(r Reason) error {
		return &MountError{Source: source, Resolved: resolved, Reason: r}
	}

	if info, err := fsys.Stat(resolved); err != nil {
		return &MountError{Source: source, Resolved: resolved, Reason: ReasonUnresolvable, Err: err}
	} else if info.Mode()&fs.ModeSocket != 0 {
		return reject(ReasonSocket)
	}

	if home == "" {
		return fmt.Errorf("check mount %q: home directory unknown", source)
	}
	realHome, err := fsys.EvalSymlinks(filepath.Clean(home))
	if err != nil {
		return fmt.Errorf("check mount %q: resolve home directory: %w", source, err)
	}

	ids := &identities{fsys: fsys, info: map[string]fs.FileInfo{}}
	switch {
	case ids.same(resolved, realHome):
		return reject(ReasonHome)
	case ids.within(realHome, resolved):
		return reject(ReasonHomeParent)
	}
	for _, root := range systemRoots {
		if ids.same(resolved, root) {
			return reject(ReasonSystem)
		}
	}
	for _, tree := range systemTrees {
		if ids.within(resolved, tree) {
			return reject(ReasonSystem)
		}
	}
	for _, rel := range secretsUnderHome {
		for _, target := range withResolved(fsys, filepath.Join(realHome, rel)) {
			if ids.overlaps(resolved, target) {
				return reject(ReasonSecrets)
			}
		}
	}
	for _, rel := range runtimeSocketDirsUnderHome {
		for _, target := range withResolved(fsys, filepath.Join(realHome, rel)) {
			if ids.overlaps(resolved, target) {
				return reject(ReasonRuntimeSocket)
			}
		}
	}
	for _, dir := range runtimeSocketDirs {
		for _, target := range withResolved(fsys, dir) {
			if ids.overlaps(resolved, target) {
				return reject(ReasonRuntimeSocket)
			}
		}
	}
	return nil
}

// CheckMounts checks every mount of a spec and returns all rejections joined.
func CheckMounts(fsys FS, home string, mounts []Mount) error {
	var errs []error
	for _, m := range mounts {
		if err := CheckMount(fsys, home, m.Source); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// withResolved returns a path and, when it is a symbolic link that resolves,
// the place it leads to. A secrets directory is often a link into a dotfiles
// directory, so both must be kept out of an environment.
func withResolved(fsys FS, path string) []string {
	resolved, err := fsys.EvalSymlinks(path)
	if err != nil || fold(resolved) == fold(path) {
		return []string{path}
	}
	return []string{path, resolved}
}

// fold normalizes a path for comparison: cleaned and lower-cased.
func fold(p string) string { return strings.ToLower(filepath.Clean(p)) }

// identities compares paths by file identity: two paths are the same when the
// filesystem says they are the same file, so a case variant on a
// case-insensitive volume and a precomposed against a decomposed Unicode name
// are equal. A path that cannot be examined, such as one that does not exist,
// is compared by its lower-cased string instead.
type identities struct {
	fsys FS
	info map[string]fs.FileInfo // nil for a path that could not be examined
}

func (c *identities) stat(path string) fs.FileInfo {
	if fi, ok := c.info[path]; ok {
		return fi
	}
	fi, err := c.fsys.Stat(path)
	if err != nil {
		fi = nil
	}
	c.info[path] = fi
	return fi
}

// same reports whether two paths are the same file.
func (c *identities) same(a, b string) bool {
	ia, ib := c.stat(a), c.stat(b)
	if ia != nil && ib != nil {
		return c.fsys.SameFile(ia, ib)
	}
	return fold(a) == fold(b)
}

// within reports whether path is dir or lies below it: some ancestor of path,
// or path itself, is the same file as dir.
func (c *identities) within(path, dir string) bool {
	for p := filepath.Clean(path); ; {
		if c.same(p, dir) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

// overlaps reports whether either path is inside the other.
func (c *identities) overlaps(a, b string) bool { return c.within(a, b) || c.within(b, a) }
