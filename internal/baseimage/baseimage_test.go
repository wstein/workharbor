package baseimage

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/runtime"
)

func TestContainerfilesArePinnedAndHoldOnlyGitAndCertificates(t *testing.T) {
	digest := regexp.MustCompile(`(?m)^FROM docker\.io/library/(fedora|ubuntu)@sha256:[0-9a-f]{64}$`)
	for _, d := range []Distro{Fedora, Ubuntu} {
		cf, err := Containerfile(d)
		if err != nil {
			t.Fatal(err)
		}
		text := string(cf)
		if !digest.MatchString(text) || !strings.Contains(text, "FROM docker.io/library/"+string(d)+"@") {
			t.Errorf("%s: the base is not pinned by digest:\n%s", d, text)
		}
		if n := len(regexp.MustCompile(`(?m)^FROM `).FindAllString(text, -1)); n != 1 {
			t.Errorf("%s: want one stage, have %d", d, n)
		}
		for _, want := range []string{"git", "ca-certificates"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: no %s", d, want)
			}
		}
		for _, banned := range []string{"curl", "wget", "claude", "codex", "sudo", "ssh", "USER ", "ADD ", "COPY "} {
			if strings.Contains(text, banned) {
				t.Errorf("%s: %q is more than git and certificates (D44)", d, banned)
			}
		}
	}
}

func TestTagIsStableAndPerBase(t *testing.T) {
	a, err := Tag(Fedora)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Tag(Fedora)
	u, _ := Tag(Ubuntu)
	if a != b || a == u || !strings.HasPrefix(a, "whr.invalid/whr-base/fedora:") || !runtime.ValidImage(a) || !runtime.ValidImage(u) {
		t.Errorf("tags: %s %s %s", a, b, u)
	}
	if _, err := Tag("alpine"); !errors.Is(err, ErrUnknownDistro) {
		t.Errorf("Tag(alpine) err = %v, want ErrUnknownDistro", err)
	}
}

type fakeBuilder struct {
	have   map[string]bool
	builds []runtime.BuildSpec
	fail   error
	seen   []byte // the Containerfile at build time
}

func (f *fakeBuilder) HasImage(_ context.Context, tag string) (bool, error) { return f.have[tag], nil }

func (f *fakeBuilder) Build(_ context.Context, b runtime.BuildSpec) ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	f.builds = append(f.builds, b)
	f.seen, _ = os.ReadFile(b.Dockerfile) //nolint:gosec // a file the test's Ensure just wrote
	if f.fail != nil {
		return []byte("step 2 failed: dnf"), f.fail
	}
	f.have[b.Tag] = true
	return nil, nil
}

func TestEnsureBuildsOnceAndAgainOnlyWhenThePinChanges(t *testing.T) {
	fb := &fakeBuilder{have: map[string]bool{}}
	dir := t.TempDir()
	tag, built, err := Ensure(context.Background(), fb, Fedora, dir)
	if err != nil || !built || len(fb.builds) != 1 {
		t.Fatalf("first start: %q built=%v err=%v builds=%d", tag, built, err, len(fb.builds))
	}
	want, _ := Containerfile(Fedora)
	if string(fb.seen) != string(want) || fb.builds[0].Tag != tag || len(fb.builds[0].Args) != 0 {
		t.Errorf("the build did not use the shipped Containerfile: %+v", fb.builds[0])
	}
	if ents, _ := os.ReadDir(fb.builds[0].ContextDir); len(ents) != 0 {
		t.Errorf("the build context must be empty, has %d entries", len(ents))
	}
	// A second start finds the image and builds nothing.
	tag2, built, err := Ensure(context.Background(), fb, Fedora, dir)
	if err != nil || built || tag2 != tag || len(fb.builds) != 1 {
		t.Errorf("second start: %q built=%v err=%v builds=%d", tag2, built, err, len(fb.builds))
	}
	// Another base is its own image.
	if _, built, _ := Ensure(context.Background(), fb, Ubuntu, dir); !built || len(fb.builds) != 2 {
		t.Errorf("ubuntu must build its own image")
	}
	// A changed pin is a changed tag, which is not there yet.
	delete(fb.have, tag)
	if _, built, _ := Ensure(context.Background(), fb, Fedora, dir); !built {
		t.Error("a missing image is built again")
	}
}

func TestEnsureReportsAFailedBuildWithItsOutput(t *testing.T) {
	fb := &fakeBuilder{have: map[string]bool{}, fail: errors.New("exit status 1")}
	_, _, err := Ensure(context.Background(), fb, Ubuntu, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "step 2 failed: dnf") || !strings.Contains(err.Error(), "whr.invalid/whr-base/ubuntu:") {
		t.Errorf("err = %v", err)
	}
	if _, _, err := Ensure(context.Background(), fb, "alpine", t.TempDir()); !errors.Is(err, ErrUnknownDistro) {
		t.Errorf("err = %v, want ErrUnknownDistro", err)
	}
}
