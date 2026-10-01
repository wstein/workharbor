package runtime

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
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
	// Env is the environment's own variables, from the repository's
	// containerEnv. A name the supervisor sets or the agent reads is refused.
	Env map[string]string

	// Egress is the proxy sidecar that gives the internal network its only way
	// out (design §7.2). Nil means no way out at all.
	Egress *Egress
	// Alternates are the cache object directories a --shared topic borrows
	// from (issue #45). Prepare mounts each read-only at its host path.
	Alternates []string
}

// Egress describes the logging allowlist proxy that runs in a sidecar attached
// to the default and the environment's internal network.
type Egress struct {
	Image string   // the stock image the proxy runs in
	Proxy string   // the host path of the proxy binary, mounted read-only into the sidecar
	Allow []string // host names the agent may reach through the proxy
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
	// networkRe is a network name; it cannot start with '-', so it is never
	// read as a flag.
	networkRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,62}$`)
	// imageRe is an image reference: name, optional tag and digest.
	imageRe = regexp.MustCompile(`^[a-z0-9][A-Za-z0-9_./:@-]{0,254}$`)
	// labelKeyRe is a label key the caller may set.
	labelKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,127}$`)
)

// ReservedLabelPrefix marks the labels the runtime adapter sets itself, such as
// the owner, the role and the network: a spec may not set one, or Delete could
// be pointed at a network the adapter never created.
const ReservedLabelPrefix = "workharbor."

// ValidImage reports whether ref is an image reference the adapter passes on.
func ValidImage(ref string) bool { return imageRe.MatchString(ref) }

// unsplittable reports whether a path is safe in a `source:target[:ro]`
// mount value: container 1.5.0 refuses a ':' in either path ("invalid volume
// format"), so it is refused here with a clear message. A ',' is taken
// verbatim by -v (checked with a source named "c,readonly=false").
func unsplittable(p string) bool { return !strings.Contains(p, ":") }

// Validate checks that the spec is hardened and well formed. It does not look
// at the host filesystem: bind mount sources are vetted by CheckMounts.
func (s Spec) Validate() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if s.Image == "" {
		add("image is required")
	} else if !ValidImage(s.Image) {
		add("image %q is not an image reference", s.Image)
	}
	if !ownerRe.MatchString(s.Owner) {
		add("owner %q must be a lower-case label value", s.Owner)
	}
	for k, v := range s.Labels {
		switch {
		case strings.HasPrefix(strings.ToLower(k), ReservedLabelPrefix):
			add("label key %q is reserved for the runtime adapter", k)
		case !labelKeyRe.MatchString(k):
			add("label key %q is not allowed", k)
		case strings.ContainsFunc(v, unicode.IsControl):
			add("label %q has a control character in its value", k)
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
	} else if s.Network.Name != "" && !networkRe.MatchString(s.Network.Name) {
		add("network name %q is not valid", s.Network.Name)
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

	problems = append(problems, checkEnv("environment variable", s.Env)...)

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
			} else if !unsplittable(m.Source) {
				add("mount %d: bind source %q holds ':'", i, m.Source)
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

// CheckMountsWithin is CheckMounts that also requires every bind mount to lie
// inside one of the workspace roots the supervisor owns.
func (s Spec) CheckMountsWithin(fsys FS, home string, roots []string) error {
	return CheckMountsWithin(fsys, home, roots, s.Mounts)
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
	case !unsplittable(target):
		return fmt.Sprintf("target %q holds ':'", target)
	case path.Clean(target) != target:
		return fmt.Sprintf("target %q must be a clean path", target)
	case seen[target]:
		return fmt.Sprintf("target %q is used twice", target)
	}
	seen[target] = true
	return ""
}
