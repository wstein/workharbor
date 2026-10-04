package hostgit

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const testToken = "ghs_TESTtokenNOTreal0123456789abcdef"

// fillCmd is `git credential fill` for github.com with the token pipe attached,
// run in the same hardened environment and with the same helper a push uses.
func fillCmd(t *testing.T, g *Git, token string) (*exec.Cmd, func()) {
	t.Helper()
	ta, err := tokenArgs("https://github.com/o/n.git")
	if err != nil {
		t.Fatal(err)
	}
	cmd := g.command(context.Background(), g.home, false, nil, append(ta, "credential", "fill")...)
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

// authSeen records the Authorization header of every request a test server gets.
type authSeen struct {
	mu   sync.Mutex
	auth []string
}

func (a *authSeen) add(r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.auth = append(a.auth, r.Header.Get("Authorization"))
}

func (a *authSeen) sawToken() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, h := range a.auth {
		if strings.HasPrefix(h, "Basic ") {
			return true
		}
	}
	return false
}

// The helper answers only for the host the push goes to: a redirect from it to
// another host gets no credential (and git would not follow it anyway), and the
// direct request still gets the token. Two loopback http servers stand in for
// the forge and for the host it redirects to; git runs in the hardened
// environment, so no keychain is involved.
func TestTheHelperIsScopedToThePushHostAndRedirectsGetNoCredential(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	var other, direct authSeen
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other.add(r)
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(b.Close)
	bu, _ := url.Parse(b.URL)
	// "localhost" is another host than 127.0.0.1 to git's credential matching
	target := "http://localhost:" + bu.Port() + "/o/n.git/info/refs?service=git-upload-pack"
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/redirect/") {
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		direct.add(r)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte("001e# service=git-upload-pack\n00000000"))
	}))
	t.Cleanup(a.Close)

	lsRemote := func(remote string, extra ...string) error {
		ta, err := tokenArgs(remote)
		if err != nil {
			t.Fatal(err)
		}
		args := append([]string{"-c", "protocol.http.allow=always"}, ta...)
		args = append(args, extra...)
		args = append(args, "ls-remote", remote)
		out, err := g.runToken(context.Background(), g.home, []string{"GIT_ALLOW_PROTOCOL=http"}, testToken, args...)
		if bytes.Contains(out, []byte(testToken)) {
			t.Error("the token is in git's output")
		}
		return err
	}
	_ = lsRemote(a.URL + "/redirect/o/n.git")
	if other.sawToken() {
		t.Error("the host a redirect led to got the token")
	}
	// Each defence alone must hold: with redirects followed again (git's own
	// default, "initial"), only the host scoping keeps the token from the other host.
	_ = lsRemote(a.URL+"/redirect/o/n.git", "-c", "http.followRedirects=initial")
	if other.sawToken() {
		t.Error("with redirects followed, the host a redirect led to got the token")
	}
	if err := lsRemote(a.URL + "/o/n.git"); err != nil {
		t.Logf("the direct ls-remote: %v", err) // the stub's answer is minimal; the credential is what counts
	}
	if !direct.sawToken() {
		t.Error("the direct request did not get the token")
	}
}

// The scoped key and the redirect switch are each pinned exactly, so dropping
// either one fails here even when the other still protects the token.
func TestTokenArgsScopeTheHelperToTheRemoteAndTurnRedirectsOff(t *testing.T) {
	t.Parallel()
	got, err := tokenArgs("https://Example.COM:8443/o/n.git")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-c", "credential.helper=",
		"-c", "credential.https://Example.COM:8443.helper=" + pipeHelper,
		"-c", "http.followRedirects=false",
	}
	if !slices.Equal(got, want) {
		t.Errorf("tokenArgs = %q, want %q", got, want)
	}
}

func TestTokenArgsNeedAHostAndNoUserinfo(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "https://", "https://u:p@github.com/o/n.git", "/tmp/x"} {
		if _, err := tokenArgs(bad); err == nil {
			t.Errorf("tokenArgs(%q) accepted", bad)
		}
	}
}
