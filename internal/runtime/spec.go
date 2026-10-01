package runtime

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// OwnerLabel is the label every environment carries: the supervisor instance
// that owns it. An adapter lists, starts, stops and deletes only environments
// with its own owner (design §5.1).
const OwnerLabel = "workharbor.owner"

// MountKind says what a mount's Source is.
type MountKind string

const (
	// MountBind mounts a host path; CheckMount vets it first (§7.4).
	MountBind MountKind = "bind"
	// MountVolume mounts a named volume the runtime manages.
	MountVolume MountKind = "volume"
)

// Mount is an explicit mount into an environment. A mount without a Kind is a
// bind mount, the kind that needs checking.
type Mount struct {
	Kind     MountKind
	Source   string // a host path for a bind mount, a volume name for a volume
	Target   string // an absolute path in the environment
	ReadOnly bool
}

// Network places an environment on a network. An internal network blocks all
// traffic out of it, and the host has no interface on it (§7.2).
type Network struct {
	Name     string // empty means the runtime's default network
	Internal bool
}

// Spec describes an environment to provision. Validate says whether it is
// hardened enough to start an agent in.
type Spec struct {
	Image  string
	Owner  string            // the OwnerLabel value
	Labels map[string]string // further labels, such as the task and run

	CPUs     int
	MemoryMB int
	DiskMB   int // disk quota

	Network      Network
	User         string // numeric "uid" or "uid:gid"; never root
	ReadOnlyRoot bool
	CapDrop      []string // must include ALL
	Init         bool     // run an init as PID 1 (--init); always true
	Tmpfs        []string // writable in-memory directories
	Mounts       []Mount
}

// ErrInvalidSpec is matched by every error Validate returns.
var ErrInvalidSpec = errors.New("invalid environment spec")

// SpecError lists everything wrong with a Spec.
type SpecError struct {
	Problems []string
}

func (e *SpecError) Error() string {
	return "invalid environment spec: " + strings.Join(e.Problems, "; ")
}

// Is makes errors.Is(err, ErrInvalidSpec) true.
func (e *SpecError) Is(target error) bool { return target == ErrInvalidSpec }

var (
	ownerRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	volumeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)
)

// Validate checks that the spec is hardened and well formed. It does not look
// at the host filesystem: bind mount sources are vetted by CheckMounts.
func (s Spec) Validate() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if s.Image == "" {
		add("image is required")
	}
	if !ownerRe.MatchString(s.Owner) {
		add("owner %q must be a lower-case label value", s.Owner)
	}
	for k := range s.Labels {
		if k == "" || k == OwnerLabel {
			add("label key %q is not allowed", k)
		}
	}
	if s.CPUs < 1 {
		add("cpus must be at least 1")
	}
	if s.MemoryMB < 64 {
		add("memory must be at least 64 MB")
	}
	if s.DiskMB < 1 {
		add("a disk quota is required")
	}
	if s.Network.Internal && s.Network.Name == "" {
		add("an internal network needs a name")
	}
	if err := checkUser(s.User); err != "" {
		add("%s", err)
	}
	if !s.Init {
		add("init must be set: without it PID 1 ignores SIGTERM and a stop takes seconds")
	}
	if !hasAll(s.CapDrop) {
		add("capabilities must be dropped (cap-drop ALL)")
	}

	targets := map[string]bool{}
	for _, t := range s.Tmpfs {
		if msg := checkTarget(t, targets); msg != "" {
			add("tmpfs %s", msg)
		}
	}
	for i, m := range s.Mounts {
		switch m.Kind {
		case MountBind, "":
			if !path.IsAbs(m.Source) {
				add("mount %d: bind source %q must be an absolute path", i, m.Source)
			}
		case MountVolume:
			if !volumeRe.MatchString(m.Source) {
				add("mount %d: volume name %q is not valid", i, m.Source)
			}
		default:
			add("mount %d: unknown kind %q", i, m.Kind)
		}
		if msg := checkTarget(m.Target, targets); msg != "" {
			add("mount %d: %s", i, msg)
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &SpecError{Problems: problems}
}

// CheckMounts vets the bind mounts of the spec with CheckMount; volumes are
// not host paths and are skipped.
func (s Spec) CheckMounts(fsys FS, home string) error {
	return CheckMounts(fsys, home, s.Mounts)
}

func hasAll(drop []string) bool {
	for _, c := range drop {
		if strings.EqualFold(c, "ALL") {
			return true
		}
	}
	return false
}

// checkUser accepts a numeric uid or uid:gid that is not root.
func checkUser(user string) string {
	if user == "" {
		return "a user is required, and it must not be root"
	}
	parts := strings.Split(user, ":")
	if len(parts) > 2 {
		return fmt.Sprintf("user %q must be numeric uid or uid:gid", user)
	}
	for _, part := range parts {
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return fmt.Sprintf("user %q must be numeric uid or uid:gid", user)
		}
		if n == 0 {
			return fmt.Sprintf("user %q is root", user)
		}
	}
	return ""
}

// checkTarget checks an absolute, clean, unique, non-root target path.
func checkTarget(target string, seen map[string]bool) string {
	switch {
	case !path.IsAbs(target):
		return fmt.Sprintf("target %q must be an absolute path", target)
	case target == "/":
		return `target "/" is the root filesystem`
	case path.Clean(target) != target:
		return fmt.Sprintf("target %q must be a clean path", target)
	case seen[target]:
		return fmt.Sprintf("target %q is used twice", target)
	}
	seen[target] = true
	return ""
}
