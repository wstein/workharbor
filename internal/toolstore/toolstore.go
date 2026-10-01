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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	SHA256   string `json:"sha256"`
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
	platformRe = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9_]+$`)
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
	for _, p := range []struct {
		path string
		mode os.FileMode
	}{{dst, 0o555}, {filepath.Join(stage, "bin"), 0o555}, {stage, 0o555}} {
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

// Verify re-hashes every entry and reports the ones that no longer match the
// hash in their name, or are writable, or have lost their tool.
func (s *Store) Verify() []string {
	var problems []string
	entries, err := os.ReadDir(filepath.Join(s.Root, "store"))
	if err != nil {
		return []string{"cannot read the store: " + err.Error()}
	}
	for _, d := range entries {
		if strings.HasPrefix(d.Name(), ".") {
			continue
		}
		parts := strings.SplitN(d.Name(), "-", 3)
		if len(parts) < 3 || len(parts[0]) != 8 {
			problems = append(problems, fmt.Sprintf("%s: not a store entry name", d.Name()))
			continue
		}
		name := ""
		bins, _ := os.ReadDir(filepath.Join(s.Root, "store", d.Name(), "bin"))
		for _, b := range bins {
			name = b.Name()
			path := filepath.Join(s.Root, "store", d.Name(), "bin", name)
			sum, err := hashFile(path)
			switch {
			case err != nil:
				problems = append(problems, fmt.Sprintf("%s: %v", d.Name(), err))
			case !strings.HasPrefix(sum, parts[0]):
				problems = append(problems, fmt.Sprintf("%s: the content hashes to %s, not the hash in the name", d.Name(), sum[:8]))
			}
			if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o222 != 0 {
				problems = append(problems, fmt.Sprintf("%s: %s is writable", d.Name(), name))
			}
		}
		if name == "" {
			problems = append(problems, fmt.Sprintf("%s: no tool in bin", d.Name()))
		}
	}
	return problems
}
