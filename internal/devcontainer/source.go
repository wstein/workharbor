package devcontainer

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/wstein/workharbor/internal/devcontainer/feature"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// commitOf resolves ref to the commit it names. Everything after reads that
// commit, so the file the supervisor read and the tree it builds from cannot
// differ when the default branch moves between the two.
func commitOf(ctx context.Context, r Runner, ref string) (string, error) {
	if err := checkRef(ref); err != nil {
		return "", err
	}
	out, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("devcontainer: resolve %s: %w", ref, err)
	}
	sha := strings.TrimSpace(string(out))
	if !shaRe.MatchString(sha) {
		return "", fmt.Errorf("devcontainer: %s is not a commit", ref)
	}
	return sha, nil
}

// Origin says where an environment came from.
type Origin string

const (
	OriginDevcontainer Origin = "devcontainer" // .devcontainer/devcontainer.json
	OriginDockerfile   Origin = "dockerfile"   // a Dockerfile or Containerfile at the root
	OriginDefault      Origin = "default"      // an image chosen from the toolchain files
)

// Toolchain is a language version a repository pins in its own files.
type Toolchain struct {
	Name    string // "go", "node" or "python"
	Version string // digits and dots only
	File    string // the file it came from
}

// Environment is what to build or run for a repository at one commit. Every
// path is relative to the repository root, and everything was read from Commit,
// so the build cannot see a working tree.
type Environment struct {
	Origin Origin
	Commit string // the default branch's commit that every read used

	// Image is the image to run, or the base image of a build when Dockerfile is
	// empty. Dockerfile and Context are set when the environment is built.
	Image      string
	Dockerfile string
	Context    string
	BuildArgs  map[string]string

	Config    Config // the devcontainer.json, zero unless Origin is devcontainer
	Toolchain *Toolchain

	// Features are the devcontainer features Resolve fetched, checked and ordered, each
	// pinned to its manifest digest. The image is built from Image with these applied
	// (D38, issue #108); the digests key the image tag, so a moved tag changes nothing
	// until the next resolution.
	Features []feature.Resolved

	// SuggestedHosts are the package registries the repository's lockfiles
	// imply. They are suggestions: the human confirms them once per repository
	// before any is allowed (D38).
	SuggestedHosts []string

	Notes []string
}

// Built reports whether the environment has to be built: from a Dockerfile, or
// from its image with features applied.
func (e Environment) Built() bool { return e.Dockerfile != "" || len(e.Features) > 0 }

// Options choose the images Resolve falls back to. The supervisor sets them
// from its configuration; a repository cannot.
type Options struct {
	// BaseImage is the image for a repository with no devcontainer, no
	// Dockerfile and no toolchain file (D43: a pinned Fedora or Ubuntu LTS).
	BaseImage string
	// ToolchainImages maps a toolchain name to an image reference whose %s is
	// replaced by the version. A missing entry means the base image.
	ToolchainImages map[string]string
	// Features fetches and checks the features of a devcontainer.json. Nil leaves them
	// requested and not applied, with a note.
	Features *feature.Resolver
}

// DefaultToolchainImages are the official language images.
var DefaultToolchainImages = map[string]string{
	"go":     "docker.io/library/golang:%s",
	"node":   "docker.io/library/node:%s",
	"python": "docker.io/library/python:%s",
}

// dockerfiles are the file names looked for at the root, in order.
var dockerfiles = []string{"Dockerfile", "Containerfile"}

// Resolve decides how to build the environment for the repository at ref, the
// default branch (D38). The order is the one the design names: a
// devcontainer.json, else a Dockerfile or Containerfile at the root, else a
// default image chosen from the toolchain files. ref is resolved to a commit
// once, and every later read uses that commit.
func Resolve(ctx context.Context, r Runner, ref string, opt Options) (Environment, error) {
	sha, err := commitOf(ctx, r, ref)
	if err != nil {
		return Environment{}, err
	}
	env := Environment{Commit: sha}

	cfg, at, found, err := read(ctx, r, sha)
	if err != nil {
		return Environment{}, err
	}
	switch {
	case found:
		env.Origin, env.Config, env.Notes = OriginDevcontainer, cfg, cfg.Notes
		env.Image, env.BuildArgs = cfg.Image, cfg.BuildArgs
		if cfg.Dockerfile != "" {
			dir := path.Dir(at)
			env.Dockerfile = path.Join(dir, cfg.Dockerfile)
			env.Context = path.Join(dir, cmpOr(cfg.Context, "."))
			env.Image = ""
		}
	default:
		for _, name := range dockerfiles {
			if _, err := file(ctx, r, sha, name); err == nil {
				env.Origin, env.Dockerfile, env.Context = OriginDockerfile, name, "."
				break
			} else if !errors.Is(err, ErrNotFound) {
				return Environment{}, err
			}
		}
	}
	if env.Origin == "" {
		env.Origin = OriginDefault
		tc, err := detectToolchain(ctx, r, sha)
		if err != nil {
			return Environment{}, err
		}
		env.Toolchain = tc
		img, note, err := defaultImage(tc, opt)
		if err != nil {
			return Environment{}, err
		}
		env.Image = img
		if note != "" {
			env.Notes = append(env.Notes, note)
		}
	}
	if env.Image != "" && !validImage(env.Image) {
		return Environment{}, fmt.Errorf("%w: image %q is not an image reference", ErrRefused, env.Image)
	}
	if err := resolveFeatures(ctx, &env, opt); err != nil {
		return Environment{}, err
	}
	if env.SuggestedHosts, err = lockfileHosts(ctx, r, sha); err != nil {
		return Environment{}, err
	}
	return env, nil
}

// resolveFeatures fetches, checks and orders the features of the environment's
// devcontainer.json (D38, issue #108). A feature outside the safe subset, or from a
// source that needs a Decision, is left out with a note that names why; a registry that
// cannot be reached, a digest that does not match and an archive that is not safe fail
// the resolution, because an environment that silently lacks its tools is worse than one
// that does not start. Features apply to an image, so a build from a Dockerfile keeps
// them requested and not applied.
func resolveFeatures(ctx context.Context, env *Environment, opt Options) error {
	reqs := env.Config.FeatureRequests
	if len(reqs) == 0 {
		return nil
	}
	note := func(format string, args ...any) { env.Notes = append(env.Notes, fmt.Sprintf(format, args...)) }
	switch {
	case env.Dockerfile != "":
		note("features: requested, not applied: they apply to an image, and this environment is built from a Dockerfile")
		return nil
	case opt.Features == nil:
		note("features: requested, not applied: no feature resolver is configured")
		return nil
	case env.Image == "":
		note("features: requested, not applied: there is no base image to apply them to")
		return nil
	}
	var got []feature.Resolved
	for _, req := range reqs {
		f, err := opt.Features.Resolve(ctx, req)
		switch {
		case errors.Is(err, feature.ErrSourceNotAllowed):
			note("feature %s is not applied: a source outside %s needs a Decision, which is not built yet", req.ID, feature.AllowedPrefix)
		case errors.Is(err, feature.ErrRefused):
			note("feature %s is not applied: %v", req.ID, err)
		case err != nil:
			return fmt.Errorf("devcontainer feature %s: %w", req.ID, err)
		default:
			got = append(got, f)
		}
	}
	refs := make([]string, len(got))
	after := make([][]string, len(got))
	for i, f := range got {
		refs[i], after[i] = f.Ref, f.Meta.InstallsAfter
	}
	order, notes, err := feature.Order(refs, after)
	if err != nil {
		return err
	}
	for _, n := range notes {
		note("%s", n)
	}
	for _, i := range order {
		env.Features = append(env.Features, got[i])
	}
	return nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// imageRe is the image reference syntax the runtime accepts (runtime.ValidImage).
var imageRe = regexp.MustCompile(`^[a-z0-9][A-Za-z0-9_./:@-]{0,254}$`)

func validImage(ref string) bool { return imageRe.MatchString(ref) }

func defaultImage(tc *Toolchain, opt Options) (image, note string, err error) {
	if tc != nil {
		tmpl, ok := opt.ToolchainImages[tc.Name]
		if ok {
			img := fmt.Sprintf(tmpl, tc.Version)
			if validImage(img) {
				return img, "default image from " + tc.File + ": " + tc.Name + " " + tc.Version, nil
			}
			return "", "", fmt.Errorf("devcontainer: the image for %s %s is not an image reference", tc.Name, tc.Version)
		}
	}
	if opt.BaseImage == "" {
		return "", "", errors.New("devcontainer: no base image is configured for a repository without a devcontainer")
	}
	return opt.BaseImage, "default base image: the repository names no environment", nil
}

var versionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

// toolchainFiles are read in this order; the first that names a known
// toolchain with a plain version wins.
var toolchainFiles = []struct {
	file  string
	parse func(data string) (name, version string)
}{
	{"go.mod", parseGoMod},
	{".tool-versions", parseToolVersions},
	{"mise.toml", parseMise},
	{".nvmrc", func(d string) (string, string) { return "node", strings.TrimPrefix(firstLine(d), "v") }},
	{".python-version", func(d string) (string, string) { return "python", firstLine(d) }},
}

func detectToolchain(ctx context.Context, r Runner, sha string) (*Toolchain, error) {
	for _, tf := range toolchainFiles {
		data, err := file(ctx, r, sha, tf.file)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if name, v := tf.parse(string(data)); name != "" && versionRe.MatchString(v) {
			return &Toolchain{Name: name, Version: v, File: tf.file}, nil
		}
	}
	return nil, nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(l)
}

func parseGoMod(d string) (string, string) {
	for _, l := range strings.Split(d, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "go "); ok {
			return "go", strings.TrimSpace(v)
		}
	}
	return "", ""
}

// toolNames maps the names asdf and mise use to ours.
var toolNames = map[string]string{
	"go": "go", "golang": "go", "node": "node", "nodejs": "node", "python": "python",
}

func parseToolVersions(d string) (string, string) {
	for _, l := range strings.Split(d, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && toolNames[f[0]] != "" {
			return toolNames[f[0]], f[1]
		}
	}
	return "", ""
}

var miseRe = regexp.MustCompile(`^\s*"?([a-z]+)"?\s*=\s*"?([0-9][0-9.]*)"?\s*(#.*)?$`)

// parseMise reads `tool = "version"` lines of mise.toml's [tools] table. It is
// not a TOML parser: a table or a list is not read.
func parseMise(d string) (string, string) {
	inTools := false
	for _, l := range strings.Split(d, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			inTools = t == "[tools]"
			continue
		}
		if m := miseRe.FindStringSubmatch(l); inTools && m != nil && toolNames[m[1]] != "" {
			return toolNames[m[1]], m[2]
		}
	}
	return "", ""
}

// lockfiles name the hosts a repository's package managers fetch from. A
// lockfile suggests; it never allows (D38).
var lockfiles = map[string][]string{
	"go.sum":            {"proxy.golang.org", "sum.golang.org"},
	"package-lock.json": {"registry.npmjs.org"},
	"yarn.lock":         {"registry.yarnpkg.com"},
	"pnpm-lock.yaml":    {"registry.npmjs.org"},
	"poetry.lock":       {"pypi.org", "files.pythonhosted.org"},
	"uv.lock":           {"pypi.org", "files.pythonhosted.org"},
	"Pipfile.lock":      {"pypi.org", "files.pythonhosted.org"},
	"Cargo.lock":        {"crates.io", "index.crates.io", "static.crates.io"},
	"Gemfile.lock":      {"rubygems.org"},
}

func lockfileHosts(ctx context.Context, r Runner, sha string) ([]string, error) {
	root, err := lsTree(ctx, r, sha, "", false)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, e := range root {
		if e.regular() {
			for _, h := range lockfiles[e.Path] {
				set[h] = true
			}
		}
	}
	hosts := make([]string, 0, len(set))
	for h := range set {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts, nil
}
