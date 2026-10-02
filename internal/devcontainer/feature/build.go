package feature

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wstein/workharbor/internal/oci"
)

// AllowedPrefix is the one source features may come from without a Decision: the
// devcontainers organisation's own registry path (D38).
const AllowedPrefix = "ghcr.io/devcontainers/features/"

// ErrSourceNotAllowed means the feature is not from AllowedPrefix: it needs a Decision,
// kept per repository like an egress host, and is not fetched until the human allows
// it.
var ErrSourceNotAllowed = errors.New("devcontainer feature: the source is not allowed without a Decision")

// Resolved is a feature pinned to a manifest digest, checked, with its install
// options. The digest is what the image tag is keyed on and what is recorded.
type Resolved struct {
	Ref    string   // as written in the devcontainer.json
	Digest string   // the manifest's sha256: the identity of this version
	Meta   Metadata // the checked devcontainer-feature.json
	Vars   [][2]string
	// Notes are things worth telling the human about this feature.
	Notes []string

	archive []byte
}

// Resolver turns requests into pinned, checked features.
type Resolver struct {
	Client *oci.Client
	// Allowed are the reference prefixes fetched without a Decision. Default
	// AllowedPrefix. A feature outside them is ErrSourceNotAllowed unless Approved says
	// its reference was allowed for this repository.
	Allowed []string
	// Approved reports whether the human allowed this reference's source for the
	// repository (a Decision, kept like an egress answer). Optional.
	Approved func(ref string) bool
	Limits   oci.ExtractLimits
}

func (r *Resolver) allowed(ref oci.Ref, written string) bool {
	full := ref.Registry + "/" + ref.Repo
	list := r.Allowed
	if len(list) == 0 {
		list = []string{AllowedPrefix}
	}
	for _, p := range list {
		if strings.HasPrefix(full+"/", p) || strings.HasPrefix(full, p) {
			return true
		}
	}
	return r.Approved != nil && r.Approved(written)
}

// Resolve fetches one feature: the manifest by its tag or digest, the one feature layer
// by its digest with the size cap, a safe extraction to read its
// devcontainer-feature.json, the refusal rules and the options. Nothing is written
// outside a temporary directory that is removed before it returns.
func (r *Resolver) Resolve(ctx context.Context, req Request) (Resolved, error) {
	ref, err := oci.ParseRef(req.ID)
	if err != nil {
		return Resolved{}, err
	}
	if !r.allowed(ref, req.ID) {
		return Resolved{}, fmt.Errorf("%w: %s", ErrSourceNotAllowed, req.ID)
	}
	m, err := r.Client.Resolve(ctx, ref)
	if err != nil {
		return Resolved{}, err
	}
	var layer *oci.Descriptor
	for i := range m.Layers {
		if m.Layers[i].MediaType == oci.MediaFeatureLayer {
			if layer != nil {
				return Resolved{}, fmt.Errorf("%w: %s has more than one feature layer", oci.ErrManifest, req.ID)
			}
			layer = &m.Layers[i]
		}
	}
	if layer == nil {
		return Resolved{}, fmt.Errorf("%w: %s has no feature layer", oci.ErrManifest, req.ID)
	}
	var blob bytes.Buffer
	if err := r.Client.Blob(ctx, ref, *layer, &blob); err != nil {
		return Resolved{}, err
	}
	dir, err := os.MkdirTemp("", "whr-feature-")
	if err != nil {
		return Resolved{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := oci.Extract(bytes.NewReader(blob.Bytes()), dir, r.Limits); err != nil {
		return Resolved{}, err
	}
	meta, err := os.ReadFile(filepath.Join(dir, "devcontainer-feature.json")) //nolint:gosec // a file of the archive that was just extracted into a private directory
	if err != nil {
		return Resolved{}, fmt.Errorf("devcontainer feature %s: no devcontainer-feature.json: %w", req.ID, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "install.sh")); err != nil || !fi.Mode().IsRegular() {
		return Resolved{}, fmt.Errorf("devcontainer feature %s: no install.sh", req.ID)
	}
	md, err := ParseMetadata(meta)
	if err != nil {
		return Resolved{}, fmt.Errorf("%s: %w", req.ID, err)
	}
	vars, err := md.ResolveOptions(req.Options)
	if err != nil {
		return Resolved{}, fmt.Errorf("%s: %w", req.ID, err)
	}
	return Resolved{Ref: req.ID, Digest: m.Digest, Meta: md, Vars: vars, archive: blob.Bytes()}, nil
}

// shellQuote quotes a value for a POSIX shell in single quotes.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// optionsFile is the shell fragment that sets what the install script reads: the
// feature's options and the user the install runs as, root, the way the devcontainer
// CLI's generated Dockerfile does.
func (f Resolved) optionsFile() string {
	var b strings.Builder
	for _, v := range f.Vars {
		fmt.Fprintf(&b, "export %s=%s\n", v[0], shellQuote(v[1]))
	}
	for _, v := range [][2]string{{"_REMOTE_USER", "root"}, {"_REMOTE_USER_HOME", "/root"}, {"_CONTAINER_USER", "root"}, {"_CONTAINER_USER_HOME", "/root"}} {
		fmt.Fprintf(&b, "export %s=%s\n", v[0], shellQuote(v[1]))
	}
	return b.String()
}

// dockerValue quotes a containerEnv value for an ENV line: backslash and quote
// escaped, "$" left alone because a feature writes PATH=...:${PATH} on purpose.
func dockerValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// Dockerfile returns the build that applies features, already in install order, on
// top of base: one RUN per feature, then the ENV lines of their containerEnv (a tool
// is on PATH only through them). The context it expects is what Stage writes.
func Dockerfile(base string, items []Resolved) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s\nUSER root\nCOPY features/ /tmp/whr-features/\n", base)
	for i := range items {
		fmt.Fprintf(&b, "RUN set -e; cd /tmp/whr-features/%d && chmod +x install.sh && . ./whr-options.env && ./install.sh\n", i)
	}
	b.WriteString("RUN rm -rf /tmp/whr-features\n")
	for _, f := range items {
		names := make([]string, 0, len(f.Meta.ContainerEnv))
		for k := range f.Meta.ContainerEnv {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			fmt.Fprintf(&b, "ENV %s=%s\n", k, dockerValue(f.Meta.ContainerEnv[k]))
		}
	}
	return b.String()
}

// Stage writes the build context below dir: each feature extracted again, safely, to
// features/<i> with its options file, and returns the Dockerfile text for base. dir
// must exist. The archives were verified at resolution, and extraction checks them
// once more.
func Stage(dir, base string, items []Resolved, lim oci.ExtractLimits) (string, error) {
	for i, f := range items {
		d := filepath.Join(dir, "features", strconv.Itoa(i))
		if err := os.MkdirAll(d, 0o750); err != nil {
			return "", err
		}
		if err := oci.Extract(bytes.NewReader(f.archive), d, lim); err != nil {
			return "", fmt.Errorf("%s: %w", f.Ref, err)
		}
		p := filepath.Join(d, "whr-options.env")
		w, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // a path below the context the supervisor made; O_EXCL refuses a file the archive already holds
		if err != nil {
			return "", fmt.Errorf("%s: %w", f.Ref, err)
		}
		_, werr := w.WriteString(f.optionsFile())
		if err := w.Close(); werr == nil {
			werr = err
		}
		if werr != nil {
			return "", werr
		}
	}
	return Dockerfile(base, items), nil
}
