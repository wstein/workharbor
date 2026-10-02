package devcontainer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/wstein/workharbor/internal/devcontainer/feature"
	"github.com/wstein/workharbor/internal/oci"
	"github.com/wstein/workharbor/internal/runtime"
)

// Tag names the image built for this environment: the owner, and a digest of
// everything the build reads, so the same commit and arguments reuse the image
// and a new default-branch commit builds a new one.
func (e Environment) Tag(owner string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", e.Commit, e.Dockerfile, e.Context)
	names := make([]string, 0, len(e.BuildArgs))
	for k := range e.BuildArgs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(h, "%s=%s\x00", k, e.BuildArgs[k])
	}
	// the image the features are applied to and each one's manifest digest and options,
	// in install order: a moved tag changes nothing until the next resolution (D38)
	if len(e.Features) > 0 {
		fmt.Fprintf(h, "features\x00%s\x00", e.Image)
		for _, f := range e.Features {
			fmt.Fprintf(h, "%s\x00%s\x00%v\x00", f.Ref, f.Digest, f.Vars)
		}
	}
	return "whr-env/" + owner + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}

// BaseTag is the tag of the image a Dockerfile builds before the features are applied
// on top of it (D38, issue #127): the tag the environment would have without them.
func (e Environment) BaseTag(owner string) string {
	e.Features = nil
	return e.Tag(owner)
}

// FeaturesOnDockerfile reports whether the environment applies features on top of the
// image its Dockerfile builds, which takes two builds: Stage, then StageFeatures on
// the tag of the first.
func (e Environment) FeaturesOnDockerfile() bool { return e.Dockerfile != "" && len(e.Features) > 0 }

// Stage writes what the build reads below workDir, from the commit the
// environment was resolved at: the context in workDir/context and the
// Dockerfile as workDir/Dockerfile, which may have lain outside the context in
// the repository. It returns the build to hand to a runtime.Builder. The
// context is the committed files of the default branch, never a checkout.
func (e Environment) Stage(ctx context.Context, r Runner, owner, workDir string) (runtime.BuildSpec, Exported, error) {
	if !e.Built() {
		return runtime.BuildSpec{}, Exported{}, fmt.Errorf("devcontainer: the environment runs image %q and builds nothing", e.Image)
	}
	if e.Dockerfile == "" {
		return e.StageFeatures(owner, workDir, e.Image)
	}
	dockerfile, err := file(ctx, r, e.Commit, e.Dockerfile)
	if err != nil {
		return runtime.BuildSpec{}, Exported{}, fmt.Errorf("devcontainer: the Dockerfile %s: %w", e.Dockerfile, err)
	}
	ctxDir := filepath.Join(workDir, "context")
	res, err := Export(ctx, r, e.Commit, e.Context, ctxDir, Limits{})
	if err != nil {
		return runtime.BuildSpec{}, res, err
	}
	df := filepath.Join(workDir, "Dockerfile")
	if err := os.WriteFile(df, dockerfile, 0o600); err != nil {
		return runtime.BuildSpec{}, res, err
	}
	tag := e.Tag(owner)
	if e.FeaturesOnDockerfile() {
		tag = e.BaseTag(owner) // the features are a second build on top of this image
	}
	b := runtime.BuildSpec{Tag: tag, ContextDir: ctxDir, Dockerfile: df, Args: e.BuildArgs}
	return b, res, b.Validate()
}

// StageFeatures writes the build of an image with devcontainer features applied: a
// context that holds only the features, extracted again and checked, and a Dockerfile
// that installs them in order on base (D38, issues #108 and #127): the environment's
// image, or the tag of the image its Dockerfile built. The repository's own files are
// not part of it.
func (e Environment) StageFeatures(owner, workDir, base string) (runtime.BuildSpec, Exported, error) {
	ctxDir := filepath.Join(workDir, "context")
	if err := os.MkdirAll(ctxDir, 0o700); err != nil {
		return runtime.BuildSpec{}, Exported{}, err
	}
	text, err := feature.Stage(ctxDir, base, e.Features, oci.ExtractLimits{})
	if err != nil {
		return runtime.BuildSpec{}, Exported{}, err
	}
	df := filepath.Join(workDir, "Dockerfile")
	if err := os.WriteFile(df, []byte(text), 0o600); err != nil {
		return runtime.BuildSpec{}, Exported{}, err
	}
	b := runtime.BuildSpec{Tag: e.Tag(owner), ContextDir: ctxDir, Dockerfile: df}
	return b, Exported{}, b.Validate()
}

// Spec applies the environment to the supervisor's hardened spec. The repository
// chooses only the image and its own variables: the user, the network, the
// mounts, the capabilities and the limits stay the supervisor's (D38). image is
// the one to run: the built tag, or Environment.Image.
func (e Environment) Spec(base runtime.Spec, image string) (runtime.Spec, error) {
	base.Image = image
	if len(e.Config.Env) > 0 {
		env := make(map[string]string, len(base.Env)+len(e.Config.Env))
		for k, v := range e.Config.Env {
			env[k] = v
		}
		for k, v := range base.Env { // the supervisor's own win
			env[k] = v
		}
		base.Env = env
	}
	if err := base.Validate(); err != nil {
		return runtime.Spec{}, err
	}
	return base, nil
}

// PostCreate returns the post-create commands as exec requests, to run inside
// the environment as the agent user (never on the host), in order. A string
// runs through `sh -c`.
func (e Environment) PostCreate() []runtime.ExecRequest {
	reqs := make([]runtime.ExecRequest, 0, len(e.Config.PostCreate))
	for _, c := range e.Config.PostCreate {
		if c.Shell != "" {
			reqs = append(reqs, runtime.ExecRequest{Cmd: []string{"sh", "-c", c.Shell}})
		} else if len(c.Argv) > 0 {
			reqs = append(reqs, runtime.ExecRequest{Cmd: c.Argv})
		}
	}
	return reqs
}

// PreviewPorts are the ports to preview (D33): the ones the file forwards and
// the ones customizations.workharbor names.
func (e Environment) PreviewPorts() []int {
	set := map[int]bool{}
	for _, p := range e.Config.ForwardPorts {
		set[p] = true
	}
	for _, p := range e.Config.Hints.PreviewPorts {
		set[p] = true
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// Where a host request came from.
const (
	FromDevcontainer = "customizations.workharbor.egress"
	FromLockfile     = "a lockfile"
)

// LockfileDigest is the source an answer to a lockfile suggestion records.
const LockfileDigest = "lockfile"

// HostRequest is an egress host the human has not answered yet. Nothing is
// allowed by being requested or suggested: the supervisor raises a Decision,
// and only an allow, stored on the supervisor's side, opens the host (D38).
type HostRequest struct {
	Host   string
	Source string // FromDevcontainer or FromLockfile
	// Digest is what the answer will remember the request by: the digest of the
	// devcontainer.json for FromDevcontainer, LockfileDigest for FromLockfile.
	Digest string
}

// HostRequests lists the hosts to ask the human about: those the file requests
// and those the lockfiles suggest, minus those already answered (allowed or
// denied) for this repository. A host both requested and suggested is asked
// once, as requested. The list is sorted.
func (e Environment) HostRequests(answered map[string]bool) []HostRequest {
	seen := map[string]bool{}
	var out []HostRequest
	add := func(h, source, digest string) {
		if !answered[h] && !seen[h] {
			seen[h] = true
			out = append(out, HostRequest{Host: h, Source: source, Digest: digest})
		}
	}
	for _, h := range e.Config.EgressRequests {
		add(h, FromDevcontainer, e.Config.SourceDigest)
	}
	for _, h := range e.SuggestedHosts {
		add(h, FromLockfile, LockfileDigest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}
