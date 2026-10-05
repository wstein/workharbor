//go:build applecontainer

package console_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/baseimage"
	"github.com/wstein/workharbor/internal/console"
	"github.com/wstein/workharbor/internal/runtime/apple"
)

// TestConsoleImageLive builds the console image on Apple Container, finds it the
// second time, and in it, as the console's user: checks the tools are there and
// no container engine is, and plants a hook, an alias, a clean filter, a textconv
// and an fsmonitor in a repository, which plain git runs and the wrapper does not.
// No credential command runs. WHR_TEST_BASE=ubuntu or alpine runs it on another base.
//
//	go test -tags applecontainer -run TestConsoleImageLive ./internal/console
func TestConsoleImageLive(t *testing.T) {
	if _, err := exec.LookPath("container"); err != nil {
		t.Skip("the container CLI is not installed")
	}
	d := baseimage.Default
	switch os.Getenv("WHR_TEST_BASE") {
	case "ubuntu":
		d = baseimage.Ubuntu
	case "alpine":
		d = console.Alpine
	}
	a, err := apple.New("wh-console-live", apple.WithTemp("wh/runtime", "console-live"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	tag, err := console.Tag(d)
	if err != nil {
		t.Fatal(err)
	}
	if have, err := a.HasImage(ctx, tag); err != nil {
		t.Fatal(err)
	} else if have {
		if out, err := exec.CommandContext(ctx, "container", "image", "delete", tag).CombinedOutput(); err != nil { //nolint:gosec // a tag this test computed
			t.Fatalf("delete %s: %v\n%s", tag, err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command("container", "image", "delete", tag).Run() }) //nolint:gosec,noctx // cleanup of the image this test built

	if got, built, err := console.Ensure(ctx, a, d, t.TempDir()); err != nil || !built || got != tag {
		t.Fatalf("first Ensure: %q built=%v err=%v", got, built, err)
	}
	if got, built, err := console.Ensure(ctx, a, d, t.TempDir()); err != nil || built || got != tag {
		t.Fatalf("second Ensure must find the image: %q built=%v err=%v", got, built, err)
	}

	script := `
set -u
for tool in git zsh fish jq curl rg tmux make vi less script; do command -v $tool >/dev/null || echo "MISSING $tool"; done
for engine in docker podman containerd buildah; do command -v $engine >/dev/null && echo "ENGINE $engine"; done
grep -q "git of the workharbor console" "$(command -v git)" || echo "git is not the wrapper: $(command -v git)"
[ "$(id -u)" = 1000 ] || echo "uid $(id -u)"
M=/tmp/marks; mkdir -p $M
R=/tmp/r; /usr/bin/git init -q -b main $R && cd $R
echo one > a.txt
/usr/bin/git add a.txt && /usr/bin/git -c user.name=a -c user.email=a@example.com commit -q -m one
printf '#!/bin/sh\ntouch %s/hook\n' $M > .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
/usr/bin/git config core.fsmonitor "sh -c 'touch $M/fsmonitor; true'"
/usr/bin/git config filter.x.clean "sh -c 'touch $M/filter; cat'"
/usr/bin/git config diff.y.textconv "sh -c 'touch $M/textconv; cat \"\$1\"' --"
/usr/bin/git config alias.st "!touch $M/alias"
printf '*.txt filter=x diff=y\n' > .git/info/attributes
run() { # label, git command
	rm -f $M/*
	$2 status >/dev/null 2>&1
	echo two >> a.txt; $2 add a.txt >/dev/null 2>&1
	$2 -c user.name=a -c user.email=a@example.com commit -q -m c >/dev/null 2>&1
	$2 diff HEAD~1 >/dev/null 2>&1
	$2 st >/dev/null 2>&1
	echo "$1: $(ls $M | tr '\n' ' ')"
	/usr/bin/git reset -q --hard HEAD~1 2>/dev/null
}
run plain /usr/bin/git
run wrapper git
git config --global alias.hi '!echo hello'
echo "human alias: $(git hi)"
`
	out, err := exec.CommandContext(ctx, "container", "run", "--rm", "--user", "1000:1000", "-e", "HOME=/home/workharbor", tag, "sh", "-c", script).Output() //nolint:gosec // tag and script are constants of this test
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	text := string(out)
	for _, bad := range []string{"MISSING", "ENGINE", "is not the wrapper", "uid "} {
		if strings.Contains(text, bad) {
			t.Errorf("the console image is wrong (%q):\n%s", bad, text)
		}
	}
	plain, wrapper := lineWith(text, "plain:"), lineWith(text, "wrapper:")
	for _, marker := range []string{"hook", "fsmonitor", "filter", "textconv", "alias"} {
		if !strings.Contains(plain, marker) {
			t.Errorf("plain git did not run the planted %s: the plant does not work (%q)", marker, plain)
		}
	}
	if strings.TrimSpace(strings.TrimPrefix(wrapper, "wrapper:")) != "" {
		t.Errorf("the wrapper let something run: %q", wrapper)
	}
	if !strings.Contains(text, "human alias: hello") {
		t.Errorf("the human's own alias does not work:\n%s", text)
	}
}

func lineWith(text, prefix string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}
