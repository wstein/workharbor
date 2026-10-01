package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

// BuildSpec is an image to build from a directory the supervisor wrote, never
// from a host checkout (design D38). The context holds the files of a commit of
// the default branch and nothing else.
type BuildSpec struct {
	Tag        string            // the image name to give the result
	ContextDir string            // an absolute directory
	Dockerfile string            // an absolute file path, which may lie outside the context
	Args       map[string]string // build arguments; the supervisor's variables are refused
}

// ErrInvalidBuild is matched by every error BuildSpec.Validate returns.
var ErrInvalidBuild = errors.New("invalid image build")

// BuildError lists everything wrong with a BuildSpec.
type BuildError struct{ Problems []string }

func (e *BuildError) Error() string { return "invalid image build: " + strings.Join(e.Problems, "; ") }

// Is makes errors.Is(err, ErrInvalidBuild) true.
func (e *BuildError) Is(target error) bool { return target == ErrInvalidBuild }

// Validate checks that the spec is well formed.
func (b BuildSpec) Validate() error {
	var problems []string
	if !ValidImage(b.Tag) {
		problems = append(problems, "tag "+quote(b.Tag)+" is not an image reference")
	}
	for name, p := range map[string]string{"context": b.ContextDir, "dockerfile": b.Dockerfile} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			problems = append(problems, name+" must be a clean absolute path")
		}
	}
	problems = append(problems, checkEnv("build argument", b.Args)...)
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &BuildError{Problems: problems}
}

func quote(s string) string { return `"` + s + `"` }

// Builder builds an image on the runtime. Build output is returned for the
// log, and a failed build returns the error with the last of it.
type Builder interface {
	Build(ctx context.Context, spec BuildSpec) (output []byte, err error)
}
