// Package toolstore builds the shared, immutable tool store (design §5.6, D19):
// agent CLIs and the supervisor's own helpers live once on the host, content
// addressed, and are mounted read-only into every environment.
//
//	store/<hash8>-<name>-<version>-<platform>/bin/<name>
//	profiles/<profile>/bin/<name> -> ../../../store/<entry>/bin/<name>
//
// A download is checked against a hash pinned in this repository and against the
// vendor's own SHA-256 manifest; a mismatch is refused and nothing is stored.
// Adding a tool is a developer action, never an agent action.
package toolstore

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Errors of the store.
var (
	ErrChecksum = errors.New("the download does not match its checksum")
	ErrBadName  = errors.New("not an accepted tool name, version or platform")
	ErrNoPin    = errors.New("no pinned tool with that name and version")
	ErrInsecure = errors.New("a tool is downloaded over https only")
	ErrTooLarge = errors.New("the download is larger than the store accepts")
)

// DefaultMaxBytes caps one download; an agent CLI is a few hundred MB.
const DefaultMaxBytes = 1 << 30

//go:embed pins.json
var pinsJSON []byte

// Pin is a tool at a version whose hash is fixed in this repository.
type Pin struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	BaseURL  string `json:"base_url"` // <base>/<version>/<platform>/<name> and <base>/<version>/manifest.json
	SHA256   string `json:"sha256"`   // the tool as stored; for an archive format, the extracted file
	// Format names how the vendor releases the tool; empty is Claude Code's
	// (a binary and a SHA-256 manifest). FormatAntigravity adds ArchiveURL and
	// SHA512 (of the archive); BaseURL is then the manifest directory, which only
	// the pin-update script reads.
	Format     string `json:"format,omitempty"`
	ArchiveURL string `json:"archive_url,omitempty"`
	SHA512     string `json:"sha512,omitempty"`
}

// Pins returns the pinned tools.
func Pins() ([]Pin, error) { return parsePins(pinsJSON) }

// LoadPins reads pins from a file instead of the built-in ones, for a developer
// who pins a newer version before it is released.
func LoadPins(path string) ([]Pin, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the developer names the file
	if err != nil {
		return nil, err
	}
	return parsePins(raw)
}

func parsePins(raw []byte) ([]Pin, error) {
	var f struct {
		Tools []Pin `json:"tools"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("toolstore: pins: %w", err)
	}
	return f.Tools, nil
}

// Libc names the C library of an environment's base, which decides the build
// of a tool that links against it (D43, #162).
type Libc string

// The two libcs a guest base has.
const (
	Glibc Libc = "glibc" // Fedora, Ubuntu: the first-class bases
	Musl  Libc = "musl"  // Alpine: non-default until #161 passes
)

// GuestPlatform is the pin platform of the build for a base with this libc.
// It has no caller yet: serve.toolProfile always takes the glibc build because
// baseimage has no Alpine base; the first caller comes with one (#161).
func GuestPlatform(l Libc) string {
	if l == Musl {
		return "linux-arm64-musl"
	}
	return "linux-arm64"
}

// ProfileName is the name of the profile made for the pins of one build: each
// tool and its version in the order given, joined by "-", with "-musl" last for
// a musl build so it cannot replace the glibc one. A build of Claude Code alone
// is claude-<version>.
func ProfileName(pins ...Pin) string {
	var parts []string
	musl := false
	for _, p := range pins {
		parts = append(parts, p.Name+"-"+p.Version)
		musl = musl || strings.HasSuffix(p.Platform, "-musl")
	}
	n := strings.Join(parts, "-")
	if musl {
		n += "-musl"
	}
	return n
}

// ProfileFor picks, from profile names, the one built for the libc of the
// environment's base: a name ending in "-musl" is a musl build, any other a
// glibc one. It returns false unless exactly one fits.
func ProfileFor(names []string, l Libc) (string, bool) {
	var hit []string
	for _, n := range names {
		if strings.HasSuffix(n, "-musl") == (l == Musl) {
			hit = append(hit, n)
		}
	}
	if len(hit) != 1 {
		return "", false
	}
	return hit[0], true
}

// Entry is a tool in the store.
type Entry struct {
	Name, Version, Platform string
	SHA256                  string
	Dir                     string // store/<hash8>-<name>-<version>-<platform>
}

// Path is the executable inside the entry.
func (e Entry) Path() string { return filepath.Join(e.Dir, "bin", e.Name) }

// Store is a tool store rooted at a directory.
type Store struct {
	Root   string
	Client *http.Client
	// MaxBytes caps one download; DefaultMaxBytes when zero.
	MaxBytes int64

	allowHTTP bool // tests only: httptest serves plain http
}

var (
	nameRe     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)
	versionRe  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,40}$`)
	platformRe = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9_]+(-musl)?$`)
	hashRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func checkNames(name, version, platform string) error {
	if !nameRe.MatchString(name) || !versionRe.MatchString(version) || !platformRe.MatchString(platform) {
		return fmt.Errorf("%w: %q %q %q", ErrBadName, name, version, platform)
	}
	return nil
}

// client returns the HTTP client with a redirect check: a redirect to a URL
// that is not https is refused before it is followed, so no request leaves
// over plain http. The caller's client is copied, never changed.
func (s *Store) client() *http.Client {
	c := http.Client{Timeout: 30 * time.Minute}
	if s.Client != nil {
		c = *s.Client
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("toolstore: too many redirects")
		}
		return s.checkScheme(req.URL.String())
	}
	return &c
}

// Download fetches a pinned tool and adds it to the store. The file's SHA-256
// must equal the pin and the vendor's manifest entry for the platform; either
// mismatch is ErrChecksum and nothing is stored.
func (s *Store) Download(ctx context.Context, p Pin) (Entry, error) {
	if err := checkNames(p.Name, p.Version, p.Platform); err != nil {
		return Entry{}, err
	}
	if !hashRe.MatchString(p.SHA256) {
		return Entry{}, fmt.Errorf("%w: the pin has no SHA-256", ErrChecksum)
	}
	switch p.Format {
	case "":
	case FormatAntigravity:
		return s.downloadAntigravity(ctx, p)
	default:
		return Entry{}, fmt.Errorf("%w: unknown pin format %q", ErrBadName, p.Format)
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if err := s.checkScheme(base); err != nil {
		return Entry{}, err
	}
	manifest, err := s.vendorChecksum(ctx, base, p)
	if err != nil {
		return Entry{}, err
	}
	if manifest != p.SHA256 {
		return Entry{}, fmt.Errorf("%w: the vendor manifest says %s for %s %s %s, the pin says %s", ErrChecksum, manifest, p.Name, p.Version, p.Platform, p.SHA256)
	}

	if err := os.MkdirAll(filepath.Join(s.Root, "store"), 0o750); err != nil {
		return Entry{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Join(s.Root, "store"), ".download-")
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	file := filepath.Join(tmp, p.Name)
	if err := s.fetch(ctx, fmt.Sprintf("%s/%s/%s/%s", base, p.Version, p.Platform, p.Name), file); err != nil {
		return Entry{}, err
	}
	return s.install(p.Name, p.Version, p.Platform, file, p.SHA256)
}

// checkScheme refuses a URL that is not https, also after a redirect.
func (s *Store) checkScheme(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("toolstore: %w", err)
	}
	if u.Scheme == "https" || (s.allowHTTP && u.Scheme == "http") {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInsecure, u.Redacted())
}

func (s *Store) maxBytes() int64 {
	if s.MaxBytes > 0 {
		return s.MaxBytes
	}
	return DefaultMaxBytes
}

// vendorChecksum reads the vendor's manifest and returns the SHA-256 it lists
// for the platform.
func (s *Store) vendorChecksum(ctx context.Context, base string, p Pin) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/manifest.json", base, p.Version), nil)
	if err != nil {
		return "", err
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("toolstore: the vendor manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := s.checkScheme(resp.Request.URL.String()); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("toolstore: the vendor manifest answered %d", resp.StatusCode)
	}
	var m struct {
		Platforms map[string]struct {
			Checksum string `json:"checksum"`
		} `json:"platforms"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&m); err != nil {
		return "", fmt.Errorf("toolstore: the vendor manifest: %w", err)
	}
	sum := m.Platforms[p.Platform].Checksum
	if !hashRe.MatchString(sum) {
		return "", fmt.Errorf("%w: the vendor manifest has no checksum for %s", ErrChecksum, p.Platform)
	}
	return sum, nil
}

// fetch downloads url to path, at most MaxBytes. The hash is checked when the
// file is installed, on the bytes that are copied into the store.
func (s *Store) fetch(ctx context.Context, src, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return fmt.Errorf("toolstore: download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := s.checkScheme(resp.Request.URL.String()); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("toolstore: download answered %d", resp.StatusCode)
	}
	limit := s.maxBytes()
	if resp.ContentLength > limit {
		return fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, resp.ContentLength, limit)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700) //nolint:gosec // a fresh file in our own temp directory
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("toolstore: download: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("%w: more than %d bytes", ErrTooLarge, limit)
	}
	return nil
}

// AddFile adds a file the developer built locally (the launcher whr-shim) to
// the store, content-addressed by the bytes that were copied.
func (s *Store) AddFile(name, version, platform, path string) (Entry, error) {
	if err := checkNames(name, version, platform); err != nil {
		return Entry{}, err
	}
	return s.install(name, version, platform, path, "")
}

// install copies a file into a staging entry, hashing what it copies, and
// places it as store/<hash8>-<name>-<version>-<platform>/bin/<name>,
// atomically and read-only. With want set, a copy of other content is
// ErrChecksum: the hash is of the bytes stored, so a file changed after an
// earlier check cannot slip in. An entry that already exists with the same
// hash is left as it is.
func (s *Store) install(name, version, platform, file, want string) (Entry, error) {
	if err := os.MkdirAll(filepath.Join(s.Root, "store"), 0o750); err != nil {
		return Entry{}, err
	}
	stage, err := os.MkdirTemp(filepath.Join(s.Root, "store"), ".stage-")
	if err != nil {
		return Entry{}, err
	}
	defer func() { makeWritable(stage); _ = os.RemoveAll(stage) }()
	if err := os.Mkdir(filepath.Join(stage, "bin"), 0o750); err != nil {
		return Entry{}, err
	}
	dst := filepath.Join(stage, "bin", name)
	sum, err := copyFile(file, dst)
	if err != nil {
		return Entry{}, err
	}
	if want != "" && sum != want {
		return Entry{}, fmt.Errorf("%w: downloaded %s, expected %s", ErrChecksum, sum, want)
	}
	dir := filepath.Join(s.Root, "store", fmt.Sprintf("%s-%s-%s-%s", sum[:8], name, version, platform))
	e := Entry{Name: name, Version: version, Platform: platform, SHA256: sum, Dir: dir}
	if _, err := os.Stat(e.Path()); err == nil {
		if got, herr := hashFile(e.Path()); herr == nil && got == sum {
			return e, nil
		}
		return Entry{}, fmt.Errorf("%w: %s exists with other content", ErrChecksum, dir)
	}
	// The full hash is kept in the entry, next to the tool: the name carries only
	// the first eight hex digits, 32 bits, which is a label and not a check.
	if err := os.WriteFile(filepath.Join(stage, RecordedHashFile), []byte(sum+"\n"), 0o444); err != nil { //nolint:gosec // a public hash, read-only
		return Entry{}, err
	}
	for _, p := range []struct {
		path string
		mode os.FileMode
	}{{dst, 0o555}, {filepath.Join(stage, "bin"), 0o555}, {filepath.Join(stage, RecordedHashFile), 0o444}, {stage, 0o555}} {
		if err := os.Chmod(p.path, p.mode); err != nil {
			return Entry{}, err
		}
	}
	if err := os.Rename(stage, dir); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// copyFile copies src to a fresh dst and returns the SHA-256 of what it wrote.
func copyFile(src, dst string) (string, error) {
	in, err := os.Open(src) //nolint:gosec // a file we just wrote or the developer named
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700) //nolint:gosec // a fresh file in our own staging directory
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), in); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // a store entry
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// makeWritable lets a read-only tree be removed.
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o750) //nolint:gosec // our own staging directory
		}
		return nil
	})
}

// Profile makes profiles/<profile>/bin/<tool> a relative link to the store
// entry of each tool, replacing the profile as one move: an upgrade is a new
// entry and a profile change, a rollback is a profile change (design §5.6).
func (s *Store) Profile(profile string, entries ...Entry) error {
	if !nameRe.MatchString(strings.ReplaceAll(profile, ".", "-")) {
		return fmt.Errorf("%w: profile %q", ErrBadName, profile)
	}
	profiles := filepath.Join(s.Root, "profiles")
	if err := os.MkdirAll(profiles, 0o750); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(profiles, ".stage-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := os.Mkdir(filepath.Join(stage, "bin"), 0o750); err != nil {
		return err
	}
	for _, e := range entries {
		target := filepath.Join("..", "..", "..", "store", filepath.Base(e.Dir), "bin", e.Name)
		if err := os.Symlink(target, filepath.Join(stage, "bin", e.Name)); err != nil {
			return err
		}
	}
	final := filepath.Join(profiles, profile)
	old := final + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Lstat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, final); err != nil {
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

// RecordedHashFile is the file in a store entry that holds the tool's full
// SHA-256, written when the entry was made.
const RecordedHashFile = "sha256"

// Problem is something Verify found wrong in the store.
type Problem struct {
	Entry string // the store entry, or empty for the store itself
	Msg   string
	// Severe means the tool must not be trusted: its content, its mode or its
	// shape is not what was installed. A problem that is not severe says only
	// that a check could not be made.
	Severe bool
}

func (p Problem) String() string {
	if p.Entry == "" {
		return p.Msg
	}
	return p.Entry + ": " + p.Msg
}

// Verify re-hashes every entry against its full SHA-256 and reports what does not
// match. The hash is the one recorded in the entry (RecordedHashFile); an entry
// from before it was recorded is checked against the built-in pin of the same
// tool, version and platform, and, if there is neither, only against the eight
// digits in its name, which is reported as a problem that is not severe. It also
// reports a tool that is writable, that has been replaced by a link or anything but
// a regular file, an entry that is itself a link, and a tool that has gone. Files
// are opened without following a link.
func (s *Store) Verify() []Problem {
	var problems []Problem
	// The store and profiles directories must be real directories: a link is
	// resolved by the host but, in the guest, through the image's rootfs. The
	// root itself may be a link (an operator's choice).
	if info, err := os.Lstat(filepath.Join(s.Root, "store")); err == nil && !info.IsDir() {
		return []Problem{{Entry: "store", Msg: "the store directory " + filepath.Join(s.Root, "store") + " is not a real directory (a link is refused)", Severe: true}}
	}
	entries, err := os.ReadDir(filepath.Join(s.Root, "store"))
	if err != nil {
		return []Problem{{Msg: "cannot read the store: " + err.Error(), Severe: true}}
	}
	pins, _ := Pins()
	for _, d := range entries {
		if strings.HasPrefix(d.Name(), ".") {
			continue
		}
		bad := func(severe bool, format string, args ...any) {
			problems = append(problems, Problem{Entry: d.Name(), Msg: fmt.Sprintf(format, args...), Severe: severe})
		}
		parts := strings.SplitN(d.Name(), "-", 3)
		if len(parts) < 3 || len(parts[0]) != 8 || !d.IsDir() {
			bad(true, "not a store entry (a directory named <hash8>-<name>-<version>-<platform>)")
			continue
		}
		dir := filepath.Join(s.Root, "store", d.Name())
		want := recordedHash(dir, parts[0])
		if want.err != "" {
			bad(true, "%s", want.err)
		}
		// A directory belongs to a pin by name, version and platform, whatever its
		// first eight digits say; a different eight digits is a look-alike.
		var pinned string
		if p, ok := pinFor(pins, d.Name()); ok {
			pinned = p.SHA256
			if parts[0] != p.SHA256[:8] {
				bad(true, "the directory is for the pinned %s %s %s but its hash %s differs from the pin %s", p.Name, p.Version, p.Platform, parts[0], p.SHA256[:8])
			}
		}
		bins, _ := os.ReadDir(filepath.Join(dir, "bin"))
		if len(bins) == 0 {
			bad(true, "no tool in bin")
		}
		for _, b := range bins {
			path := filepath.Join(dir, "bin", b.Name())
			if !b.Type().IsRegular() {
				bad(true, "%s is not a regular file (%v): a tool is never a link", b.Name(), b.Type())
				continue
			}
			sum, err := hashRegular(path)
			if err != nil {
				bad(true, "%v", err)
				continue
			}
			// A pin is compiled into the binary; the record and the tool are written by
			// the same user, so whoever can change one can change both. A pin therefore
			// decides whenever there is one, and the record only for a tool without a
			// pin (one added from a file).
			switch {
			case pinned != "":
				if sum != pinned {
					bad(true, "the content hashes to %s, but the pin says %s", sum, pinned)
				}
				if want.sum != "" && want.sum != pinned {
					bad(true, "the recorded hash %s is not the pin %s", want.sum, pinned)
				}
			case want.sum != "":
				if sum != want.sum {
					bad(true, "the content hashes to %s, but %s was recorded", sum, want.sum)
				}
			case !strings.HasPrefix(sum, parts[0]):
				bad(true, "the content hashes to %s, not the hash in the name", sum[:8])
			default:
				bad(false, "no full hash is recorded and no pin matches: only the first 32 bits of the hash in the name were checked")
			}
			if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o222 != 0 {
				bad(true, "%s is writable", b.Name())
			}
		}
	}
	return append(problems, s.verifyProfiles(pins)...)
}

// pinFor returns the pin (with a hash) that a store directory name is for: the
// name is eight hex digits, then -<name>-<version>-<platform> of the pin. Tool
// names contain dashes, so the name is matched against the pins, not split.
func pinFor(pins []Pin, dirName string) (Pin, bool) {
	for _, p := range pins {
		suffix := "-" + p.Name + "-" + p.Version + "-" + p.Platform
		if len(p.SHA256) != 64 || !strings.HasSuffix(dirName, suffix) {
			continue
		}
		if prefix := strings.TrimSuffix(dirName, suffix); len(prefix) == 8 && isHex(prefix) {
			return p, true
		}
	}
	return Pin{}, false
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// verifyProfiles checks that every profile holds only the relative links
// Profile writes, each into an existing entry of this store whose tool is the
// link's own name, and that a pinned tool's link goes to the pin's own entry.
func (s *Store) verifyProfiles(pins []Pin) []Problem {
	profiles := filepath.Join(s.Root, "profiles")
	if info, err := os.Lstat(profiles); err == nil && !info.IsDir() {
		return []Problem{{Entry: "profiles", Msg: "the profiles directory " + profiles + " is not a real directory (a link is refused)", Severe: true}}
	}
	list, err := os.ReadDir(profiles)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []Problem{{Msg: "cannot read the profiles: " + err.Error(), Severe: true}}
	}
	evalRoot, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return []Problem{{Msg: "cannot resolve the store root: " + err.Error(), Severe: true}}
	}
	var problems []Problem
	for _, pd := range list {
		if strings.HasPrefix(pd.Name(), ".") {
			continue
		}
		if !pd.IsDir() {
			problems = append(problems, Problem{Entry: "profiles/" + pd.Name(), Msg: "not a profile directory", Severe: true})
			continue
		}
		binDir := filepath.Join(profiles, pd.Name(), "bin")
		if info, err := os.Lstat(binDir); err != nil || !info.IsDir() {
			problems = append(problems, Problem{Entry: "profiles/" + pd.Name(), Msg: "bin is not a real directory (a link or a file is refused)", Severe: true})
			continue
		}
		links, err := os.ReadDir(binDir)
		if err != nil {
			problems = append(problems, Problem{Entry: "profiles/" + pd.Name(), Msg: "cannot read bin: " + err.Error(), Severe: true})
			continue
		}
		for _, l := range links {
			name := "profiles/" + pd.Name() + "/bin/" + l.Name()
			bad := func(format string, args ...any) {
				problems = append(problems, Problem{Entry: name, Msg: fmt.Sprintf(format, args...), Severe: true})
			}
			if l.Type()&fs.ModeSymlink == 0 {
				bad("not a link (%v): a profile holds only links into the store", l.Type())
				continue
			}
			target, err := os.Readlink(filepath.Join(binDir, l.Name()))
			if err != nil {
				bad("%v", err)
				continue
			}
			seg := strings.Split(target, "/")
			if len(seg) != 7 || seg[0] != ".." || seg[1] != ".." || seg[2] != ".." || seg[3] != "store" || seg[5] != "bin" ||
				seg[4] == "" || seg[4] == "." || seg[4] == ".." || strings.HasPrefix(seg[4], ".") || seg[6] != l.Name() {
				bad("the link target %q is not ../../../store/<entry>/bin/%s", target, l.Name())
				continue
			}
			entry := filepath.Join(s.Root, "store", seg[4])
			if info, err := os.Lstat(entry); err != nil || !info.IsDir() {
				bad("the link points to %s, which is not an entry of the store", seg[4])
				continue
			}
			if info, err := os.Lstat(filepath.Join(entry, "bin", l.Name())); err != nil || !info.Mode().IsRegular() {
				bad("the entry %s has no tool %s", seg[4], l.Name())
				continue
			}
			want := filepath.Join(evalRoot, "store", seg[4], "bin", l.Name())
			if got, err := filepath.EvalSymlinks(filepath.Join(binDir, l.Name())); err != nil || got != want {
				bad("the link resolves to %q, not into the entry's own bin (%s)", got, want)
				continue
			}
			var pinned, covered bool
			for _, p := range pins {
				if len(p.SHA256) != 64 || p.Name != l.Name() {
					continue
				}
				pinned = true
				if !strings.HasSuffix(seg[4], "-"+p.Name+"-"+p.Version+"-"+p.Platform) {
					continue
				}
				covered = true
				if seg[4] != p.SHA256[:8]+"-"+p.Name+"-"+p.Version+"-"+p.Platform {
					bad("the link points to %s, not the pin's own entry %s-%s-%s-%s", seg[4], p.SHA256[:8], p.Name, p.Version, p.Platform)
				}
			}
			if pinned && !covered {
				bad("%s is a pinned tool and no pin covers the entry %s", l.Name(), seg[4])
				continue
			}
			if sum, err := hashRegular(filepath.Join(entry, "bin", l.Name())); err != nil || len(seg[4]) < 8 || !strings.HasPrefix(sum, seg[4][:8]) {
				bad("the entry %s does not hold the content its name says", seg[4])
				continue
			}
		}
	}
	return problems
}

// recorded is the hash read from an entry's RecordedHashFile.
type recorded struct {
	sum string // empty when there is none
	err string // set when there is one that cannot be trusted
}

// recordedHash reads the full hash recorded in an entry. A missing file is not an
// error (an older entry); one that is a link, not a hash, or not the hash whose
// first digits name the entry is.
func recordedHash(dir, hash8 string) recorded {
	path := filepath.Join(dir, RecordedHashFile)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return recorded{}
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128 {
		return recorded{err: RecordedHashFile + " is not a small regular file"}
	}
	raw, err := os.ReadFile(path) //nolint:gosec // a store entry, checked above to be a small regular file
	if err != nil {
		return recorded{err: err.Error()}
	}
	sum := strings.TrimSpace(string(raw))
	if !hashRe.MatchString(sum) || !strings.HasPrefix(sum, hash8) {
		return recorded{err: "the recorded hash is not a SHA-256 that starts with the hash in the name"}
	}
	return recorded{sum: sum}
}

// hashRegular hashes a regular file, opened without following a link.
func hashRegular(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // a store entry
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
