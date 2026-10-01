package hostgit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// Cache is the bare repository the supervisor keeps per repository. Topics are
// cloned from it, so one object store serves every task. The agent never
// writes it: its objects are mounted read-only where a topic needs them.
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
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if _, err := g.InitBare(ctx, path); err != nil {
			return nil, err
		}
	} else if _, err := g.OpenBare(ctx, path); err != nil {
		return nil, err
	}
	return &Cache{g: g, path: path, cfg: cfg}, nil
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
	if !validBranch(branch) {
		return fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	if by <= 0 {
		return fmt.Errorf("deepen by %d: must be positive", by)
	}
	return c.fetch(ctx, "--deepen="+strconv.Itoa(by), "--", c.cfg.Source, refspec(branch))
}

// Topic is a checkout the supervisor created for one line of work.
type Topic struct {
	Path   string // the checkout
	Branch string // the topic branch, checked out
	// Alternates are the cache directories the checkout borrows objects from:
	// the cache's objects directory at full depth, none for a shallow clone.
	// The runtime mounts them read-only at their host path.
	Alternates []string
}

// CloneTopic creates the checkout of a topic: a clone of the cache at base
// with its own .git (config, refs) and an empty template, so no hooks are
// copied, on a new branch topic. At full depth it is a shared clone whose
// alternates name the cache's objects directory; with a positive clone_depth
// it is a self-contained shallow clone, because --shared is ignored for a
// shallow source (design §4.5). dest must not exist yet.
func (c *Cache) CloneTopic(ctx context.Context, dest, base, topic string) (*Topic, error) {
	if !validBranch(base) || !validBranch(topic) {
		return nil, fmt.Errorf("%w: %q or %q", ErrBadBranch, base, topic)
	}
	parent := filepath.Dir(dest)
	if err := checkDir(parent); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(dest); err == nil {
		return nil, fmt.Errorf("%w: %q already exists", ErrBadPath, dest)
	}
	shallow, err := c.IsShallow(ctx)
	if err != nil {
		return nil, err
	}
	if c.cfg.CloneDepth == 0 && shallow {
		return nil, ErrShallowCache
	}

	args := []string{"clone", "--quiet", "--no-tags", "--template=", "--branch", base}
	source := c.path
	if c.cfg.CloneDepth > 0 {
		args = append(args, "--depth="+strconv.Itoa(c.cfg.CloneDepth))
		source = "file://" + c.path // a plain path ignores --depth
	} else {
		args = append(args, "--shared")
	}
	if _, err := c.g.run(ctx, parent, true, nil, append(args, "--", source, dest)...); err != nil {
		return nil, err
	}
	if _, err := c.g.run(ctx, dest, false, nil, "checkout", "--quiet", "-b", topic); err != nil {
		return nil, err
	}

	t := &Topic{Path: dest, Branch: topic}
	if c.cfg.CloneDepth == 0 {
		// A shared clone must borrow from the cache and nothing else; if git
		// ignored --shared the topic would hold a private copy and the mount
		// would be the wrong thing to hand out.
		data, err := os.ReadFile(filepath.Join(dest, ".git", "objects", "info", "alternates")) //nolint:gosec // inside the clone just made
		if err != nil || strings.TrimSpace(string(data)) != c.ObjectsDir() {
			return nil, fmt.Errorf("%w: the clone does not borrow from %s", ErrBadPath, c.ObjectsDir())
		}
		t.Alternates = []string{c.ObjectsDir()}
	}
	return t, nil
}

// FetchTarget copies the target branch from the cache into this repository,
// so that the supervisor's copy of a topic and its target can be compared and
// rebased. A shallow history is accepted.
func (r *Repo) FetchTarget(ctx context.Context, c *Cache, branch string, deepen int) error {
	if !validBranch(branch) {
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
			if err := c.Deepen(ctx, target, o.Step); err != nil {
				return "", err
			}
		}
		if err := r.FetchTarget(ctx, c, target, o.Step); err != nil {
			return "", err
		}
	}
}
