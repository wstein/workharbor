// Package baseimage is the workharbor base image of design D44: the image an
// environment runs when its repository has no devcontainer. It is a first-class
// base (D43), Fedora by default or Ubuntu LTS, pinned by digest, plus git and CA
// certificates. The Containerfiles ship in the binary, so the image is the same
// on every host and changes only with a release. whr builds it once, through
// the same builder as a repository's own image (#76), and builds it again only
// when a Containerfile, and so its pinned digest, changes.
package baseimage

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wstein/workharbor/internal/runtime"
)

//go:embed Containerfile.fedora Containerfile.ubuntu
var files embed.FS

// Distro names a first-class base.
type Distro string

// The first-class bases (D43).
const (
	Fedora Distro = "fedora" // the default
	Ubuntu Distro = "ubuntu" // the LTS alternative
)

// Default is the base used when the configuration names none.
const Default = Fedora

// ErrUnknownDistro is returned for a base that is not first-class.
var ErrUnknownDistro = errors.New("baseimage: not a first-class base (want fedora or ubuntu)")

// Valid reports whether d is a first-class base.
func (d Distro) Valid() bool { return d == Fedora || d == Ubuntu }

// Containerfile returns the Containerfile of a base.
func Containerfile(d Distro) ([]byte, error) {
	if !d.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownDistro, string(d))
	}
	return files.ReadFile("Containerfile." + string(d))
}

// Tag is the stable name of the image built from a base: it carries a digest of
// the Containerfile, which holds the pinned digest of the stock image, so it
// stays the same until the pin changes.
func Tag(d Distro) (string, error) {
	cf, err := Containerfile(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cf)
	return runtime.BuiltImageHost + "whr-base/" + string(d) + ":" + hex.EncodeToString(sum[:])[:12], nil
}

// Builder builds an image and says whether one exists.
type Builder interface {
	runtime.Builder
	// HasImage reports whether the runtime has an image with this tag.
	HasImage(ctx context.Context, tag string) (bool, error)
}

// Ensure makes sure the base image of d exists and returns its tag: nothing is
// built when the image is already there, so a start after the first one is
// quick. built says whether this call built it. workDir is a directory the
// supervisor owns; the Containerfile and an empty context are written below it.
func Ensure(ctx context.Context, b Builder, d Distro, workDir string) (tag string, built bool, err error) {
	if tag, err = Tag(d); err != nil {
		return "", false, err
	}
	cf, err := Containerfile(d)
	if err != nil {
		return "", false, err
	}
	built, err = EnsureImage(ctx, b, tag, cf, nil, filepath.Join(workDir, string(d)))
	return tag, built, err
}

// EnsureImage builds the image tag from a Containerfile and the files of its
// build context (name to content, no directories), unless the runtime already
// has an image with that tag. It is what Ensure and the console image share.
// dir is a directory the supervisor owns; the Containerfile and the context
// are written below it. The caller chooses a tag that changes whenever any of
// the inputs does.
func EnsureImage(ctx context.Context, b Builder, tag string, containerfile []byte, contextFiles map[string][]byte, dir string) (built bool, err error) {
	have, err := b.HasImage(ctx, tag)
	if err != nil {
		return false, fmt.Errorf("baseimage: look for %s: %w", tag, err)
	}
	if have {
		return false, nil
	}
	contextDir := filepath.Join(dir, "context")
	if err := os.MkdirAll(contextDir, 0o700); err != nil {
		return false, err
	}
	for name, content := range contextFiles {
		if name != filepath.Base(name) || name == "" || name == "." || name == ".." {
			return false, fmt.Errorf("baseimage: %q is not a plain file name for a build context", name)
		}
		if err := os.WriteFile(filepath.Join(contextDir, name), content, 0o600); err != nil {
			return false, err
		}
	}
	file := filepath.Join(dir, "Containerfile")
	if err := os.WriteFile(file, containerfile, 0o600); err != nil {
		return false, err
	}
	if out, err := b.Build(ctx, runtime.BuildSpec{Tag: tag, ContextDir: contextDir, Dockerfile: file}); err != nil {
		return false, fmt.Errorf("baseimage: build %s: %w\n%s", tag, err, tail(out, 2000))
	}
	return true, nil
}

// tail returns the last n bytes of the builder's output, the part that says why
// a build failed.
func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}
