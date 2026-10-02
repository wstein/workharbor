package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/termproto"
)

const (
	sshTestHost = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHostHostHostHostHostHostHostHostHostHost whr-console"
	sshTestCert = "ssh-ed25519-cert-v01@openssh.com AAAACertCertCert"
)

func certReply() string {
	return ok(`{"certificate":"` + sshTestCert + `","host_key":"` + sshTestHost + `","principal":"whr","expires_at":"2026-10-02T12:10:00Z"}`)
}

// runSSHCLI runs whr with a fake ssh and a private state directory.
func (s *stub) runSSHCLI(dir string, fakeSSH func(args []string) int, stdin io.Reader, args ...string) (code int, stdout, stderr string) {
	s.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: stdin, Stdout: &out, Stderr: &errOut,
		Getenv: func(k string) string {
			if k == "WHR_SSH_DIR" {
				return dir
			}
			return ""
		},
		NewClient:  func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil },
		Executable: func() (string, error) { return "/opt/whr bin/whr", nil },
	}
	if fakeSSH != nil {
		env.SSH = func(_ context.Context, a []string) (int, error) { return fakeSSH(a), nil }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code = Execute(ctx, env, args)
	return code, out.String(), errOut.String()
}

func TestSSHConfigPrintsABlockAndAsksNothing(t *testing.T) {
	s := newStub(t)
	dir := filepath.Join(t.TempDir(), "ssh")
	code, out, errOut := s.runSSHCLI(dir, nil, strings.NewReader(""), "ssh", "--config", "--forward")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	for _, want := range []string{
		"Host whr-console", "User whr", "ProxyCommand '/opt/whr bin/whr' ssh --proxy --forward",
		"IdentityFile " + filepath.Join(dir, "id_ed25519"), "CertificateFile " + filepath.Join(dir, "id_ed25519-cert.pub"),
		"UserKnownHostsFile " + filepath.Join(dir, "known_hosts"), "StrictHostKeyChecking yes", "IdentitiesOnly yes", "ForwardAgent no",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the block lacks %q:\n%s", want, out)
		}
	}
	if len(s.req) != 0 {
		t.Errorf("--config reached the supervisor: %+v", s.req)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("--config made files")
	}
}

func TestSSHGetsACertificateForAKeyThatStaysHereAndRunsSSHWithOnlyThat(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/console", 200, ok(`{"env_id":"env-1","read_write":[],"reused":true}`))
	s.reply("POST /v1/console/ssh/certificate", 200, certReply())
	dir := filepath.Join(t.TempDir(), "ssh")
	var got []string
	code, _, errOut := s.runSSHCLI(dir, func(a []string) int { got = a; return 0 }, strings.NewReader(""), "ssh", "--", "ls", "-la")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	// The key was made here, private, and only its public half was sent.
	info, err := os.Stat(filepath.Join(dir, "id_ed25519"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key: %v, %v", info, err)
	}
	priv, _ := readTest(filepath.Join(dir, "id_ed25519"))
	pub, _ := readTest(filepath.Join(dir, "id_ed25519.pub"))
	reqs := s.requests("POST /v1/console/ssh/certificate")
	if len(reqs) != 1 || !strings.Contains(reqs[0].body, strings.TrimSpace(string(pub))) || strings.Contains(reqs[0].body, "PRIVATE") || strings.Contains(reqs[0].body, strings.Split(string(priv), "\n")[1]) {
		t.Fatalf("the request = %+v", reqs)
	}
	if !strings.Contains(reqs[0].body, `"forwarding":false`) {
		t.Errorf("forwarding is on by default: %s", reqs[0].body)
	}
	if b, _ := readTest(filepath.Join(dir, "id_ed25519-cert.pub")); strings.TrimSpace(string(b)) != sshTestCert {
		t.Errorf("certificate file = %q", b)
	}
	if b, _ := readTest(filepath.Join(dir, "known_hosts")); strings.TrimSpace(string(b)) != "whr-console "+sshTestHost {
		t.Errorf("known_hosts = %q", b)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{
		"-F /dev/null", "IdentitiesOnly=yes", "-i " + filepath.Join(dir, "id_ed25519"), "CertificateFile=" + filepath.Join(dir, "id_ed25519-cert.pub"),
		"StrictHostKeyChecking=yes", "GlobalKnownHostsFile=/dev/null", "PasswordAuthentication=no", "ForwardAgent=no", "-l whr",
		"ProxyCommand='/opt/whr bin/whr' ssh --proxy",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ssh was run without %q: %v", want, got)
		}
	}
	if len(got) < 3 || got[len(got)-3] != "whr-console" || got[len(got)-2] != "ls" || got[len(got)-1] != "-la" {
		t.Errorf("the host and the command are not last: %v", got)
	}
	if len(s.requests("POST /v1/console")) != 0 {
		t.Error("an open console was opened again")
	}
	// A second run reuses the key and asks for a new certificate.
	if _, _, _ = s.runSSHCLI(dir, func([]string) int { return 0 }, strings.NewReader(""), "ssh"); len(s.requests("POST /v1/console/ssh/certificate")) != 2 {
		t.Error("the second connection did not get its own certificate")
	}
	if again, _ := readTest(filepath.Join(dir, "id_ed25519")); string(again) != string(priv) {
		t.Error("the key was replaced")
	}
}

func TestSSHOpensTheConsoleWhenNoneIsOpenAndPassesTheExitCodeOn(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/console", 200, ok(`null`))
	s.reply("POST /v1/console", 200, ok(`{"env_id":"env-1","read_write":[],"reused":false}`))
	s.reply("POST /v1/console/ssh/certificate", 200, certReply())
	code, _, _ := s.runSSHCLI(filepath.Join(t.TempDir(), "ssh"), func([]string) int { return 5 }, strings.NewReader(""), "ssh", "--forward")
	if code != 5 {
		t.Errorf("exit code = %d, want ssh's 5", code)
	}
	if opened := s.requests("POST /v1/console"); len(opened) != 1 || !strings.Contains(opened[0].body, `"read_write":[]`) {
		t.Errorf("open = %+v", opened)
	}
	if reqs := s.requests("POST /v1/console/ssh/certificate"); len(reqs) != 1 || !strings.Contains(reqs[0].body, `"forwarding":true`) {
		t.Errorf("--forward did not ask for forwarding: %+v", reqs)
	}
}

func TestSSHRefusesACertificateReplyThatIsNotOneLineOfAKey(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/console", 200, ok(`{"env_id":"e","read_write":[],"reused":true}`))
	for name, reply := range map[string]string{
		"two lines in the certificate": ok(`{"certificate":"ssh-ed25519-cert-v01@openssh.com A\nProxyCommand evil","host_key":"` + sshTestHost + `","principal":"whr","expires_at":"x"}`),
		"a host key of another type":   ok(`{"certificate":"` + sshTestCert + `","host_key":"ssh-rsa AAAA","principal":"whr","expires_at":"x"}`),
		"no certificate":               ok(`{"certificate":"","host_key":"` + sshTestHost + `","principal":"whr","expires_at":"x"}`),
	} {
		s.reply("POST /v1/console/ssh/certificate", 200, reply)
		dir := filepath.Join(t.TempDir(), "ssh")
		ran := false
		code, _, _ := s.runSSHCLI(dir, func([]string) int { ran = true; return 0 }, strings.NewReader(""), "ssh")
		if code == 0 || ran {
			t.Errorf("%s: code %d, ssh ran %v", name, code, ran)
		}
		if _, err := os.Stat(filepath.Join(dir, "known_hosts")); err == nil {
			t.Errorf("%s: a host key was pinned", name)
		}
	}
}

func TestSSHNeedsAPlaceForItsKeyAndFlagsStandAlone(t *testing.T) {
	s := newStub(t)
	var out, errOut bytes.Buffer
	env := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }, NewClient: func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil }}
	if code := Execute(context.Background(), env, []string{"ssh"}); code != exitcode.Usage {
		t.Errorf("no home: code %d", code)
	}
	env.Getenv = func(k string) string {
		if k == "WHR_SSH_DIR" {
			return "relative/dir"
		}
		return ""
	}
	if code := Execute(context.Background(), env, []string{"ssh"}); code != exitcode.Usage {
		t.Errorf("a relative WHR_SSH_DIR: code %d", code)
	}
	if code, _, _ := s.runSSHCLI(filepath.Join(t.TempDir(), "x"), nil, strings.NewReader(""), "ssh", "--proxy", "--config"); code != exitcode.Usage {
		t.Errorf("--proxy with --config: code %d", code)
	}
	if code, _, _ := s.runSSHCLI(filepath.Join(t.TempDir(), "x"), nil, strings.NewReader(""), "ssh", "--proxy", "--", "ls"); code != exitcode.Usage {
		t.Errorf("--proxy with a command: code %d", code)
	}
}

func TestSSHProxyCarriesRawBytesBothWays(t *testing.T) {
	s := newStub(t)
	s.reply("GET /v1/console", 200, ok(`{"env_id":"e","read_write":[],"reused":true}`))
	s.reply("POST /v1/console/ssh/certificate", 200, certReply())
	s.mu.Lock()
	s.h["GET /v1/console/ssh"] = func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.Header.Get("Upgrade") != termproto.SSHUpgrade {
			s.t.Errorf("Upgrade = %q", r.Header.Get("Upgrade"))
		}
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			s.t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+termproto.SSHUpgrade+"\r\n\r\n")
		b := make([]byte, 256)
		for {
			n, err := buf.Read(b)
			if n > 0 {
				_, _ = conn.Write(bytes.ToUpper(b[:n]))
			}
			if err != nil {
				return
			}
		}
	}
	s.mu.Unlock()
	pr, pw := io.Pipe()
	var out, errOut safeBuffer
	env := Env{
		Stdin: pr, Stdout: &out, Stderr: &errOut,
		Getenv:    func(k string) string { return map[string]string{"WHR_SSH_DIR": filepath.Join(t.TempDir(), "ssh")}[k] },
		NewClient: func(string) (*Client, error) { return NewClientFor(s.ts.URL, tok), nil },
	}
	done := make(chan int, 1)
	go func() { done <- Execute(context.Background(), env, []string{"ssh", "--proxy"}) }()
	if _, err := io.WriteString(pw, "SSH-2.0-OpenSSH_9.9\r\n\x00\x01"); err != nil {
		t.Fatal(err)
	}
	want := strings.ToUpper("SSH-2.0-OpenSSH_9.9\r\n\x00\x01")
	eventually(t, func() bool { return out.String() == want })
	_ = pw.Close() // ssh ends: the connection goes
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("code %d: %s", code, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the proxy did not end with ssh")
	}
	if strings.Contains(out.String(), "known_hosts") || strings.Contains(out.String(), "opening the console") {
		t.Errorf("the proxy's stdout holds more than the connection: %q", out.String())
	}
}

// safeBuffer is a buffer a goroutine writes while the test reads.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("the condition did not come true")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readTest reads a file the test's own run made.
func readTest(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // a file in the test's own temporary directory
}
