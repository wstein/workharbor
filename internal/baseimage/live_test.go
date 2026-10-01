//go:build applecontainer

package baseimage_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/baseimage"
	"github.com/wstein/workharbor/internal/runtime/apple"
)

// TestBaseImageLive builds the base image on Apple Container, a second time
// finds it instead of building, and runs git in it as the agent's user: a
// worktree and a bundle, the two things D44 needs git for. It takes a minute or
// two for the first build. WHR_TEST_BASE=ubuntu runs it on the other base.
//
//	go test -tags applecontainer -run TestBaseImageLive ./internal/baseimage
func TestBaseImageLive(t *testing.T) {
	if _, err := exec.LookPath("container"); err != nil {
		t.Skip("the container CLI is not installed")
	}
	d := baseimage.Default
	if os.Getenv("WHR_TEST_BASE") == "ubuntu" {
		d = baseimage.Ubuntu
	}
	a, err := apple.New("wh-base-live")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tag, err := baseimage.Tag(d)
	if err != nil {
		t.Fatal(err)
	}
	if have, err := a.HasImage(ctx, tag); err != nil {
		t.Fatal(err)
	} else if have {
		t.Logf("%s is already built; removing it to test the build", tag)
		if out, err := exec.CommandContext(ctx, "container", "image", "delete", tag).CombinedOutput(); err != nil { //nolint:gosec // a tag this test computed
			t.Fatalf("delete %s: %v\n%s", tag, err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command("container", "image", "delete", tag).Run() }) //nolint:gosec,noctx // cleanup of the image this test built

	if got, built, err := baseimage.Ensure(ctx, a, d, t.TempDir()); err != nil || !built || got != tag {
		t.Fatalf("first Ensure: %q built=%v err=%v", got, built, err)
	}
	if got, built, err := baseimage.Ensure(ctx, a, d, t.TempDir()); err != nil || built || got != tag {
		t.Fatalf("second Ensure must find the image: %q built=%v err=%v", got, built, err)
	}

	script := strings.Join([]string{
		"set -e",
		"git --version",
		"git init -q /tmp/r && cd /tmp/r",
		"git -c user.name=a -c user.email=a@example.com commit --allow-empty -qm one",
		"git worktree add -q -b agent/docs /tmp/wt",
		"git bundle create -q /tmp/x.bundle agent/docs",
		"git bundle verify -q /tmp/x.bundle",
		// The certificates work: git verifies GitHub's TLS certificate.
		"git ls-remote --exit-code https://github.com/octocat/Hello-World.git HEAD",
	}, "; ")
	out, err := exec.CommandContext(ctx, "container", "run", "--rm", "--user", "1000:1000", "-e", "HOME=/tmp", tag, "sh", "-c", script).Output() //nolint:gosec // tag and script are constants of this test
	if err != nil {
		t.Fatalf("git in the base image: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "git version") || !strings.Contains(string(out), "\tHEAD") {
		t.Errorf("output = %q", out)
	}
}
