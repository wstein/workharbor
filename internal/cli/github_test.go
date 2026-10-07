package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/exitcode"
)

// syncBuf is a buffer a test can read while the command still writes to it.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }

func (s *syncBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func freeAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

func runCLIAsync(args ...string) (done chan int, stdout, stderr *syncBuf) {
	stdout, stderr = &syncBuf{}, &syncBuf{}
	env := Env{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr, Getenv: func(string) string { return "" }}
	done = make(chan int, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		done <- Execute(ctx, env, args)
	}()
	return done, stdout, stderr
}

func waitFor(t *testing.T, b *syncBuf, re *regexp.Regexp) []string {
	t.Helper()
	for i := 0; i < 300; i++ {
		if m := re.FindStringSubmatch(b.String()); m != nil {
			return m
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never saw %v in %q", re, b.String())
	return nil
}

func fakeConversion(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"id":55,"slug":"workharbor-cli","html_url":"x","pem":"-----BEGIN RSA PRIVATE KEY-----\nabcdefghijklmnop\n-----END RSA PRIVATE KEY-----\n","client_secret":"client-secret-0123456789","webhook_secret":"webhook-secret-0123456789"}`)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestGitHubAppCreateWaitsForTheRedirectAndPrintsTheConfigurationLines(t *testing.T) {
	gh := fakeConversion(t)
	addr := freeAddr(t)
	keyDir := filepath.Join(t.TempDir(), "whr")
	done, stdout, stderr := runCLIAsync("github", "app", "create", "--public-url", "http://"+addr, "--listen", addr,
		"--key-dir", keyDir, "--name", "workharbor-cli", "--api-url", gh.URL, "--github-url", "http://127.0.0.1:9")
	m := waitFor(t, stderr, regexp.MustCompile(`(http://`+regexp.QuoteMeta(addr)+`/github/app/new\?state=[0-9a-f]{64})`))

	resp, err := http.Get(m[1]) //nolint:noctx,gosec // a test against the command's own server
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	cb := strings.Replace(m[1], "/new?", "/callback?code=c0de1234abcd&", 1)
	resp, err = http.Get(cb) //nolint:noctx,gosec // a test against the command's own server
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("callback: %d", resp.StatusCode)
	}
	if code := <-done; code != exitcode.OK {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	key := filepath.Join(keyDir, "github-app-55.pem")
	if want := fmt.Sprintf("app_id\t55\nkey_file\t%s\ninstall_url\thttp://127.0.0.1:9/apps/workharbor-cli/installations/new\n", key); stdout.String() != want {
		t.Errorf("stdout %q, want %q", stdout.String(), want)
	}
	if !strings.Contains(stderr.String(), `"github": {"app_id": 55, "key_file": "`+key+`"}`) || !strings.Contains(stderr.String(), "bypass") {
		t.Errorf("stderr %q", stderr.String())
	}
	if fi, err := os.Stat(key); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("key file: %v %v", fi, err)
	}
	for _, secret := range []string{"client-secret-0123456789", "webhook-secret-0123456789", "abcdefghijklmnop"} {
		if strings.Contains(stdout.String()+stderr.String(), secret) {
			t.Errorf("the command printed a secret %q", secret)
		}
	}
}

func TestGitHubAppCreateRefusesWhatIsNotSafeOrPossible(t *testing.T) {
	addr := freeAddr(t)
	run := func(args ...string) (int, string) {
		done, _, stderr := runCLIAsync(args...)
		code := <-done
		return code, stderr.String()
	}
	if code, _ := run("github", "app", "create", "--listen", addr); code != exitcode.Usage {
		t.Errorf("no --public-url: exit %d, want usage", code)
	}
	if code, _ := run("github", "app", "create", "--public-url", "http://whr.example.test", "--listen", addr); code != exitcode.Usage {
		t.Errorf("http public URL: exit %d, want usage", code)
	}
	if code, _ := run("github", "app", "create", "--public-url", "https://whr.example.test", "--listen", "0.0.0.0:8787"); code != exitcode.Usage {
		t.Errorf("a non-loopback listen address: exit %d, want usage", code)
	}
	if code, _ := run("github", "app", "nope"); code != exitcode.Usage {
		t.Errorf("unknown subcommand: exit %d, want usage", code)
	}

	// the address is taken, as when `whr serve` runs: a plain error, not a usage error
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	code, errOut := run("github", "app", "create", "--public-url", "https://whr.example.test", "--listen", addr, "--key-dir", t.TempDir())
	if code != exitcode.Error || !strings.Contains(errOut, "whr serve") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestGitHubAppCreateTakesThePublicNameFromTheConfigurationAndAddsHTTPS(t *testing.T) {
	addr := freeAddr(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	body := fmt.Sprintf(`{"listen": %q, "api_token_file": "/x", "public_url": "WHR.Example.test/"}`, addr)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(addr)
	done, _, stderr := runCLIAsync("github", "app", "create", "--config", cfg, "--ttl", "1s", "--key-dir", dir)
	<-done
	out := stderr.String()
	if !strings.Contains(out, "https://whr.example.test/github/app/new?state=") {
		t.Errorf("the link does not use the normalised public name: %q", out)
	}
	if !strings.Contains(out, "If the link times out") || !strings.Contains(out, "tailscale serve --bg "+port) || !strings.Contains(out, "Do not use `tailscale funnel`") {
		t.Errorf("no hint for a link that times out: %q", out)
	}

	// a name that could steer the redirect is refused before anything listens
	for _, bad := range []string{"http://whr.example.test", "whr.example.test/x", "a.example?b=//evil.example"} {
		done, _, stderr = runCLIAsync("github", "app", "create", "--config", cfg, "--public-url", bad)
		if code := <-done; code != exitcode.Usage || !strings.Contains(stderr.String(), "public name") {
			t.Errorf("%q: exit %d, stderr %q", bad, code, stderr.String())
		}
	}
}

func TestGitHubAppCreateWithoutAPublicNameSaysWhatToDo(t *testing.T) {
	addr := freeAddr(t)
	done, _, stderr := runCLIAsync("github", "app", "create", "--listen", addr)
	if code := <-done; code != exitcode.Usage {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"public_url", "--public-url", "--local"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, stderr.String())
		}
	}
	done, _, stderr = runCLIAsync("github", "app", "create", "--listen", addr, "--local", "--ttl", "1s", "--key-dir", t.TempDir())
	<-done
	if !strings.Contains(stderr.String(), "http://"+addr+"/github/app/new?state=") || !strings.Contains(stderr.String(), "on this Mac") {
		t.Errorf("--local: %q", stderr.String())
	}
}
