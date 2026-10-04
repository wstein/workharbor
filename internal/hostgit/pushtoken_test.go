package hostgit

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "ghs_TESTtokenNOTreal0123456789abcdef"

// fillCmd is `git credential fill` for github.com with the token pipe attached,
// run in the same hardened environment and with the same helper a push uses.
func fillCmd(t *testing.T, g *Git, token string) (*exec.Cmd, func()) {
	t.Helper()
	cmd := g.command(context.Background(), g.home, false, nil, "-c", "credential.helper="+pipeHelper, "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	release, err := attachToken(cmd, token)
	if err != nil {
		t.Fatal(err)
	}
	return cmd, release
}

func TestHelperReceivesTheTokenThroughThePipe(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	cmd, release := fillCmd(t, g, testToken)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	release()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("git credential fill: %v: %s", err, errb.String())
	}
	if !strings.Contains(out.String(), "password="+testToken+"\n") || !strings.Contains(out.String(), "username=x-access-token\n") {
		t.Errorf("git credential fill gave %q", out.String())
	}
	// the token is in no argv and no environment of the command that carried it
	for _, s := range append(append([]string{}, cmd.Args...), cmd.Env...) {
		if strings.Contains(s, testToken) {
			t.Errorf("the token is in %q", s)
		}
	}
	if strings.Contains(pipeHelper, "ghs_") || strings.Contains(errb.String(), testToken) {
		t.Error("the token is in the helper text or standard error")
	}
}

func TestOnlyTheTokenCommandInheritsThePipe(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	_, release := fillCmd(t, g, testToken) // the pipe is open in this process, not started
	defer release()
	// a second child, started while the pipe is open here, has no file descriptor 3
	second := exec.CommandContext(context.Background(), "sh", "-c", "true >&3")
	second.Env = g.Env()
	if err := second.Run(); err == nil {
		t.Error("a second child inherited the token pipe")
	}
}

func TestPushTokenRefusesWhatIsNotAnHTTPSRemoteWithAToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newPrep(t)
	prepared, err := p.repo.Prepare(ctx, p.spec())
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(p.base, "remote.git")
	if _, err := p.g.InitBare(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if err := p.repo.PushToken(ctx, remote, "agent/topic", prepared.SHA, testToken); !errors.Is(err, ErrBadSource) {
		t.Errorf("a token to a local remote = %v", err)
	}
	if err := p.repo.Push(ctx, "https://github.com/o/n.git", "agent/topic", prepared.SHA); !errors.Is(err, ErrBadSource) {
		t.Errorf("an https push without the token path = %v", err)
	}
	for _, tok := range []string{"", "a b", "a\nb", "a\x00b"} {
		if err := p.repo.PushToken(ctx, "https://github.com/o/n.git", "agent/topic", prepared.SHA, tok); err == nil {
			t.Errorf("token %q accepted", tok)
		}
	}
	// a push to a remote that cannot be reached fails without the token in the error
	err = p.repo.PushToken(ctx, "https://127.0.0.1:1/o/n.git", "agent/topic", prepared.SHA, testToken)
	if err == nil {
		t.Fatal("a push to a closed port succeeded")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("the token is in the error: %v", err)
	}
	// the same commit pushed twice to a local remote succeeds both times
	for i := 0; i < 2; i++ {
		if err := p.repo.Push(ctx, remote, "agent/topic", prepared.SHA); err != nil {
			t.Fatalf("push %d: %v", i+1, err)
		}
	}
}
