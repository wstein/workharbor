package console

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/baseimage"
	"github.com/wstein/workharbor/internal/runtime"
)

var fromRe = regexp.MustCompile(`(?m)^FROM (docker\.io/library/(fedora|ubuntu)@sha256:[0-9a-f]{64})$`)

func TestConsoleImagesArePinnedLikeTheBaseImagesAndHaveTheTools(t *testing.T) {
	for _, d := range []baseimage.Distro{baseimage.Fedora, baseimage.Ubuntu} {
		cf, err := Containerfile(d)
		if err != nil {
			t.Fatal(err)
		}
		text := string(cf)
		m := fromRe.FindStringSubmatch(text)
		if m == nil || len(fromRe.FindAllString(text, -1)) != 1 {
			t.Fatalf("%s: want one FROM pinned by digest:\n%s", d, text)
		}
		// The console and the base image of D44 are the same stock image.
		base, _ := baseimage.Containerfile(d)
		if !strings.Contains(string(base), m[1]) {
			t.Errorf("%s: the console's base %s is not the base image's", d, m[1])
		}
		for _, tool := range []string{"git", "zsh", "fish", "jq", "curl", "ripgrep", "tmux", "make", "less", "util-linux", "ca-certificates"} {
			if !strings.Contains(text, tool) {
				t.Errorf("%s: no %s", d, tool)
			}
		}
		if !strings.Contains(text, "vim") {
			t.Errorf("%s: no editor", d)
		}
		body := regexp.MustCompile(`(?m)^#.*$`).ReplaceAllString(fromRe.ReplaceAllString(text, ""), "") // not the comments, and the registry's name is docker.io
		for _, banned := range []string{"docker", "podman", "containerd", "buildah", "ssh-agent", "sudo", "USER root", "ARG ", "ENV "} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(banned)) {
				t.Errorf("%s: %q does not belong in the console (D43: no engine, no credentials)", d, banned)
			}
		}
		if !strings.Contains(text, "COPY whr-git /usr/local/bin/git") || !strings.Contains(text, "useradd -u 1000") {
			t.Errorf("%s: the wrapper or the user is missing", d)
		}
	}
	if _, err := Containerfile("alpine"); err == nil {
		t.Error("alpine is not a first-class base")
	}
}

func TestTagChangesWithTheWrapperAndThePin(t *testing.T) {
	a, err := Tag(baseimage.Fedora)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := Tag(baseimage.Ubuntu)
	b, _ := Tag(baseimage.Fedora)
	if a != b || a == u || !strings.HasPrefix(a, "whr-console/fedora:") || !runtime.ValidImage(a) {
		t.Errorf("tags %s %s %s", a, b, u)
	}
	base, _ := baseimage.Tag(baseimage.Fedora)
	if strings.Contains(a, strings.TrimPrefix(base, "whr-base/fedora:")) {
		t.Error("the console tag must not be the base image's")
	}
}

type fakeBuilder struct {
	have   map[string]bool
	builds []runtime.BuildSpec
	files  map[string]string // the staged context, by name
	cf     string
}

func (f *fakeBuilder) HasImage(_ context.Context, tag string) (bool, error) { return f.have[tag], nil }

func (f *fakeBuilder) Build(_ context.Context, b runtime.BuildSpec) ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	f.builds = append(f.builds, b)
	cf, _ := os.ReadFile(b.Dockerfile) //nolint:gosec // a file the test's Ensure just wrote
	f.cf = string(cf)
	f.files = map[string]string{}
	entries, _ := os.ReadDir(b.ContextDir)
	for _, e := range entries {
		data, _ := os.ReadFile(b.ContextDir + "/" + e.Name()) //nolint:gosec // the test's own context
		f.files[e.Name()] = string(data)
	}
	f.have[b.Tag] = true
	return nil, nil
}

func TestEnsureStagesTheWrapperInTheContextAndBuildsOnce(t *testing.T) {
	fb := &fakeBuilder{have: map[string]bool{}}
	dir := t.TempDir()
	tag, built, err := Ensure(context.Background(), fb, baseimage.Ubuntu, dir)
	if err != nil || !built || len(fb.builds) != 1 || fb.builds[0].Tag != tag {
		t.Fatalf("first: %q built=%v err=%v builds=%d", tag, built, err, len(fb.builds))
	}
	if fb.files["whr-git"] != string(GitWrapper()) || fb.files["whr-sshd"] != string(SSHD()) || fb.files["sshd_config"] != string(SSHDConfig()) || len(fb.files) != 3 {
		t.Errorf("the context must hold exactly the wrapper, the sshd launcher and its configuration: %v", fb.files)
	}
	want, _ := Containerfile(baseimage.Ubuntu)
	if fb.cf != string(want) {
		t.Error("the build did not use the shipped Containerfile")
	}
	if _, built, err := Ensure(context.Background(), fb, baseimage.Ubuntu, dir); err != nil || built || len(fb.builds) != 1 {
		t.Errorf("second: built=%v err=%v builds=%d", built, err, len(fb.builds))
	}
	// The base image's own build is separate and does not share a directory.
	if _, built, _ := baseimage.Ensure(context.Background(), fb, baseimage.Ubuntu, dir); !built {
		t.Error("the base image must build on its own")
	}
	if _, _, err := Ensure(context.Background(), fb, "alpine", dir); err == nil {
		t.Error("an unknown base must fail")
	}
}
