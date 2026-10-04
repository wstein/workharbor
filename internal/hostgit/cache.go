package hostgit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
)

// Errors of the repository cache and the topic clones.
var (
	ErrBadSource    = errors.New("not an accepted repository source")
	ErrShallowCache = errors.New("the cache is shallow but clone_depth is 0")
	ErrNoMergeBase  = errors.New("no merge base between the target and the topic")
)

// CacheConfig is the per-repository setting of the cache (design §4.5).
type CacheConfig struct {
	// Source is where the cache is refreshed from: an absolute path, or an
	// https:// URL without credentials. Nothing else is accepted; an
	// authenticated fetch comes with the forge adapter (issue #27).
	Source string
	// CloneDepth is the repository's clone_depth. 0 keeps the full history; a
	// positive value fetches the cache with --depth, for very large repositories.
	CloneDepth int
}

// Cache is the supervisor's mirror of a forge repository (design D42, §4.5): one
// bare repository per forge repository, fed only from the forge and never from
// a workspace, never mounted into an environment. It supplies the target branch,
// its history for a merge base, and the base the imported bundles build on.
type Cache struct {
	g    *Git
	path string
	cfg  CacheConfig
}

// OpenCache opens the cache at path, creating the bare repository when the
// directory does not exist yet (its parent must).
func (g *Git) OpenCache(ctx context.Context, path string, cfg CacheConfig) (*Cache, error) {
	if cfg.CloneDepth < 0 {
		return nil, fmt.Errorf("%w: clone_depth %d", ErrBadSource, cfg.CloneDepth)
	}
	if err := validSource(cfg.Source); err != nil {
		return nil, err
	}
	// A local source inside the workspace root is agent-writable: the cache is
	// fed from the forge, never from something an agent can change.
	if strings.HasPrefix(cfg.Source, "/") {
		if src, err := resolveDir(cfg.Source); err == nil && g.inWorkspace(src) {
			return nil, fmt.Errorf("%w: %q is inside a workspace root", ErrBadSource, cfg.Source)
		}
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%w: cache path %q must be absolute and clean", ErrBadPath, path)
	}
	if g.cacheRoot != "" {
		parent, err := resolveDir(filepath.Dir(path))
		if err != nil || parent != g.cacheRoot || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
			return nil, fmt.Errorf("%w: cache %q is not a direct child of the cache root", ErrBadPath, path)
		}
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if _, err := g.InitBare(ctx, path); err != nil {
			return nil, err
		}
	} else if _, err := g.OpenBare(ctx, path); err != nil {
		return nil, err
	}
	c := &Cache{g: g, path: path, cfg: cfg}
	// Nothing in the cache expires: an agent's branch may build on an object the
	// forge no longer has (design §4.5). The writes
	// take the cache's lock: handles opened in parallel on one cache would
	// otherwise collide on git's config lock.
	unlock, err := c.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	for _, kv := range [][2]string{{"gc.pruneExpire", "never"}, {"gc.reflogExpire", "never"}, {"gc.reflogExpireUnreachable", "never"}} {
		if _, err := g.run(ctx, path, false, nil, "config", kv[0], kv[1]); err != nil {
			return nil, err
		}
	}
	return c, nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// CachePath maps a repository name, "owner/name", to its cache directory. Only
// ASCII letters, digits, ".", "_" and "-" are accepted (each part starting with
// a letter or digit, at most 100 characters), so no separator, "..", option or
// Unicode form can leave the cache root. The name is lower-cased and a short
// hash of it is appended: on a case-insensitive disk Foo/x and foo/x are one
// cache, and the mapping is stable.
func (g *Git) CachePath(repo string) (string, error) {
	if g.cacheRoot == "" {
		return "", ErrNoCacheRoot
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || !nameRe.MatchString(owner) || !nameRe.MatchString(name) {
		return "", fmt.Errorf("%w: %q", ErrBadName, repo)
	}
	lower := strings.ToLower(owner) + "/" + strings.ToLower(name)
	sum := sha256.Sum256([]byte(lower))
	return filepath.Join(g.cacheRoot, strings.ToLower(owner)+"__"+strings.ToLower(name)+"-"+hex.EncodeToString(sum[:4])+".git"), nil
}

// Path returns where the cache lives.
func (c *Cache) Path() string { return c.path }

// ObjectsDir is the directory a full-depth topic borrows its objects from. It
// is what the runtime mounts read-only, and what hostgit lists as an allowed
// alternate (WithAlternates).
func (c *Cache) ObjectsDir() string { return filepath.Join(c.path, "objects") }

// validSource accepts an absolute local path or an https URL without
// credentials. Everything else could reach a host program or a service the
// agent must not: ssh, ext::, file:// and the scp-like forms, and anything that
// would be read as an option.
func validSource(src string) error {
	bad := func(why string) error { return fmt.Errorf("%w: %q (%s)", ErrBadSource, src, why) }
	switch {
	case src == "":
		return bad("empty")
	case strings.HasPrefix(src, "-"):
		return bad("starts with a dash")
	case strings.ContainsAny(src, "\n\r\x00 \t"):
		return bad("contains whitespace")
	case strings.HasPrefix(src, "/"):
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			return bad("not a directory")
		}
		return nil
	case strings.HasPrefix(src, "https://"):
		u, err := url.Parse(src)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return bad("not a plain https URL")
		}
		return nil
	}
	return bad("only an absolute path or an https URL")
}

// netArgs returns the options that let git use the protocol of a source: the
// file transport for a path, https for a URL, and nothing else.
func netArgs(src string) (extraEnv, args []string) {
	if strings.HasPrefix(src, "https://") {
		return []string{"GIT_ALLOW_PROTOCOL=file:https"}, []string{"-c", "protocol.https.allow=always"}
	}
	return nil, nil
}

func (c *Cache) fetch(ctx context.Context, extra ...string) error {
	env, pre := netArgs(c.cfg.Source)
	args := append(pre, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head")
	args = append(args, extra...)
	_, err := c.g.run(ctx, c.path, true, env, args...)
	return err
}

func refspec(branch string) string { return "+refs/heads/" + branch + ":refs/heads/" + branch }

// IsShallow reports whether the cache has a truncated history.
func (c *Cache) IsShallow(ctx context.Context) (bool, error) {
	return isShallow(ctx, c.g, c.path)
}

func isShallow(ctx context.Context, g *Git, dir string) (bool, error) {
	out, err := g.run(ctx, dir, false, nil, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

// Refresh fetches one branch from the source into the cache. The refspec is
// forced, because a shallow cache cannot fast-forward (a plain fetch is
// rejected as non-fast-forward), no tags and no submodules are fetched, and
// every object is checked. A positive clone_depth fetches with --depth; with 0
// an earlier shallow cache is made complete.
func (c *Cache) Refresh(ctx context.Context, branch string) error {
	unlock, err := c.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return c.refresh(ctx, branch)
}

func (c *Cache) refresh(ctx context.Context, branch string) error {
	if !validBranch(branch) {
		return fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	var opts []string
	switch shallow, err := c.IsShallow(ctx); {
	case err != nil:
		return err
	case c.cfg.CloneDepth > 0:
		opts = append(opts, "--depth="+strconv.Itoa(c.cfg.CloneDepth))
	case shallow:
		opts = append(opts, "--unshallow")
	}
	return c.fetch(ctx, append(opts, "--", c.cfg.Source, refspec(branch))...)
}

// Deepen fetches the branch again with a history that reaches by more
// commits past the cache's shallow boundary.
func (c *Cache) Deepen(ctx context.Context, branch string, by int) error {
	unlock, err := c.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return c.deepen(ctx, branch, by)
}

func (c *Cache) deepen(ctx context.Context, branch string, by int) error {
	if !validBranch(branch) {
		return fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	if by <= 0 {
		return fmt.Errorf("deepen by %d: must be positive", by)
	}
	return c.fetch(ctx, "--deepen="+strconv.Itoa(by), "--", c.cfg.Source, refspec(branch))
}

// Maintain packs the cache: the only explicit maintenance, since background gc
// and maintenance are off. It runs under the cache lock, and the cache
// expires nothing.
func (c *Cache) Maintain(ctx context.Context) error {
	unlock, err := c.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	_, err = c.g.run(ctx, c.path, false, nil, "gc", "--quiet")
	return err
}

// FetchTarget copies the target branch from the cache into this repository,
// so that the supervisor's copy of a topic and its target can be compared and
// rebased. A shallow history is accepted.
func (r *Repo) FetchTarget(ctx context.Context, c *Cache, branch string, deepen int) error {
	unlock, err := c.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return r.fetchTarget(ctx, c, branch, deepen)
}

func (r *Repo) fetchTarget(ctx context.Context, c *Cache, branch string, deepen int) error {
	if !validBranch(branch) || domain.InAgentNamespace(branch) {
		return fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	args := []string{"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--update-shallow"}
	if deepen > 0 {
		args = append(args, "--deepen="+strconv.Itoa(deepen))
	}
	_, err := r.g.run(ctx, r.path, true, nil, append(args, "--", c.path, refspec(branch))...)
	return err
}

// IsShallow reports whether this repository has a truncated history.
func (r *Repo) IsShallow(ctx context.Context) (bool, error) { return isShallow(ctx, r.g, r.path) }

// MergeBase returns the best common ancestor of two refs, and false when there
// is none in the history this repository has.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, bool, error) {
	out, err := r.g.run(ctx, r.path, false, nil, "merge-base", a, b)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(out)), true, nil
}

// DeepenOptions bound the search for a merge base.
type DeepenOptions struct {
	Step int // commits to deepen by each round
	Max  int // commits to deepen by in all
}

// EnsureMergeBase makes sure a rebase of the topic branch onto the target
// branch (names, not refs) has a merge base
// (design §4.5). When the history is complete or there is a base it returns at
// once. Otherwise, while either side is shallow, it deepens the cache and then
// the supervisor's copy by Step and looks again, up to Max, and returns
// ErrNoMergeBase when it gives up, so the caller opens a Decision instead of
// rebasing blindly: with no merge base a rebase would replay the whole shallow
// history.
func (c *Cache) EnsureMergeBase(ctx context.Context, r *Repo, target, topic string, o DeepenOptions) (string, error) {
	if !validBranch(target) || !validBranch(topic) {
		return "", fmt.Errorf("%w: %q or %q", ErrBadBranch, target, topic)
	}
	unlock, err := c.lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	if o.Step <= 0 {
		o.Step = 50
	}
	if o.Max <= 0 {
		o.Max = 1000
	}
	for deepened := 0; ; deepened += o.Step {
		base, ok, err := r.MergeBase(ctx, "refs/heads/"+target, "refs/heads/"+topic)
		if err != nil {
			return "", err
		}
		if ok {
			return base, nil
		}
		cacheShallow, err := c.IsShallow(ctx)
		if err != nil {
			return "", err
		}
		repoShallow, err := r.IsShallow(ctx)
		if err != nil {
			return "", err
		}
		if (!cacheShallow && !repoShallow) || deepened >= o.Max {
			return "", fmt.Errorf("%w: %s and %s after deepening by %d", ErrNoMergeBase, target, topic, deepened)
		}
		if cacheShallow {
			if err := c.deepen(ctx, target, o.Step); err != nil {
				return "", err
			}
		}
		if err := r.fetchTarget(ctx, c, target, o.Step); err != nil {
			return "", err
		}
	}
}

// ValidRepoName reports whether a repository name, "owner/name", is accepted by
// CachePath: a configuration is checked with it at start.
func ValidRepoName(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	return ok && nameRe.MatchString(owner) && nameRe.MatchString(name)
}
