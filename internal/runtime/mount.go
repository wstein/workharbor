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
	".claude",
	".codex",
	"Library/Keychains",
}

// runtimeSocketDirs are directories where container runtimes and daemons keep
// their sockets, relative to the home directory or absolute.
var (
	runtimeSocketDirsUnderHome = []string{".socktainer", ".docker/run"}
	runtimeSocketDirs          = []string{"/var/run", "/private/var/run", "/run"}
)

// systemRoots are directories that are rejected exactly, because they hold
// every user or every volume; systemTrees are rejected with everything below.
var (
	systemRoots = []string{"/", "/Users", "/home", "/private", "/var", "/private/var", "/Volumes", "/Library"}
	systemTrees = []string{"/etc", "/private/etc", "/System", "/dev", "/proc", "/sys", "/boot", "/root"}
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
// Paths are compared without regard to case, as on a default APFS volume,
// even on a case-sensitive filesystem: rejecting too much is the safe side.
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

	switch {
	case same(resolved, realHome):
		return reject(ReasonHome)
	case within(realHome, resolved):
		return reject(ReasonHomeParent)
	}
	for _, root := range systemRoots {
		if same(resolved, root) {
			return reject(ReasonSystem)
		}
	}
	for _, tree := range systemTrees {
		if within(resolved, tree) {
			return reject(ReasonSystem)
		}
	}
	for _, rel := range secretsUnderHome {
		if overlaps(resolved, filepath.Join(realHome, rel)) {
			return reject(ReasonSecrets)
		}
	}
	for _, rel := range runtimeSocketDirsUnderHome {
		if overlaps(resolved, filepath.Join(realHome, rel)) {
			return reject(ReasonRuntimeSocket)
		}
	}
	for _, dir := range runtimeSocketDirs {
		if overlaps(resolved, dir) {
			return reject(ReasonRuntimeSocket)
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

// fold normalizes a path for comparison: cleaned and lower-cased.
func fold(p string) string { return strings.ToLower(filepath.Clean(p)) }

// same reports whether two paths are equal, ignoring case.
func same(a, b string) bool { return fold(a) == fold(b) }

// within reports whether path is dir or lies below it, ignoring case.
func within(path, dir string) bool {
	path, dir = fold(path), fold(dir)
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// overlaps reports whether either path is inside the other.
func overlaps(a, b string) bool { return within(a, b) || within(b, a) }
