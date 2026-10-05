//go:build applecontainer

package apple

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/sshca"
)

// execConn makes the stdin and stdout of an exec a net.Conn: what the supervisor
// does to carry SSH over its API.
type execConn struct {
	stdin  *io.PipeWriter
	st     runtime.ExecStream
	cancel context.CancelFunc
	rest   []byte
	logTxt strings.Builder
}

func (c *execConn) Read(p []byte) (int, error) {
	for len(c.rest) == 0 {
		chunk, ok := <-c.st.Chunks()
		if !ok {
			return 0, io.EOF
		}
		if chunk.Stream == runtime.Stdout {
			c.rest = chunk.Data
		} else {
			c.logTxt.Write(chunk.Data)
		}
	}
	n := copy(p, c.rest)
	c.rest = c.rest[n:]
	return n, nil
}
func (c *execConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }
func (c *execConn) Close() error {
	_ = c.stdin.Close()
	c.cancel()
	return nil
}
func (c *execConn) LocalAddr() net.Addr              { return pipeAddr{} }
func (c *execConn) RemoteAddr() net.Addr             { return pipeAddr{} }
func (c *execConn) SetDeadline(time.Time) error      { return nil }
func (c *execConn) SetReadDeadline(time.Time) error  { return nil }
func (c *execConn) SetWriteDeadline(time.Time) error { return nil }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "exec" }
func (pipeAddr) String() string  { return "console" }

// TestConsoleSSHLive runs the console's sshd (whr-sshd, from the console image:
// set WHR_TEST_IMAGE to it) as the console's user in a hardened environment, and
// connects to it over exec the way the supervisor carries a connection. A
// certificate of the authority for the principal workharbor gets a shell as workharbor; a
// certificate of another authority, a password and another principal do not;
// without the forwarding permission nothing is forwarded.
//
//	go test -tags applecontainer -run TestConsoleSSHLive ./internal/runtime/apple
func TestConsoleSSHLive(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	spec := h.NewSpec()
	spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountVolume, Source: "whtmp-conformance-sshhome", Target: "/home/workharbor"})
	id, err := h.Adapter.Provision(ctx, mustPrepare(t, h, spec))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.Adapter.Stop(context.Background(), id)
		_ = h.Adapter.Delete(context.Background(), id)
		_ = h.Adapter.RemoveVolume(context.Background(), "whtmp-conformance-sshhome")
	})
	if err := h.Adapter.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	run := func(env []string, cmd ...string) (string, string, int) {
		st, err := h.Adapter.Exec(ctx, id, runtime.ExecRequest{Cmd: cmd, Env: env})
		if err != nil {
			t.Fatal(err)
		}
		out, errOut, code, _ := runtime.Collect(st)
		return strings.TrimSpace(string(out)), string(errOut), code
	}
	if _, _, code := run(nil, "sh", "-c", "test -x /usr/local/bin/whr-sshd && test -x /usr/sbin/sshd"); code != 0 {
		t.Skip("the image has no sshd: run with WHR_TEST_IMAGE set to the console image")
	}

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca")
	if err := sshca.Generate(caPath); err != nil {
		t.Fatal(err)
	}
	ca, err := sshca.Load(caPath)
	if err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(dir, "other")
	if err := sshca.Generate(otherPath); err != nil {
		t.Fatal(err)
	}
	other, err := sshca.Load(otherPath)
	if err != nil {
		t.Fatal(err)
	}

	// Six first calls at once make one key, and its halves belong together.
	hostKeys := make(chan string, 6)
	for range 6 {
		go func() {
			out, _, _ := run([]string{"HOME=/home/workharbor"}, "/usr/local/bin/whr-sshd", "hostkey")
			hostKeys <- out
		}()
	}
	var first string
	for range 6 {
		k := <-hostKeys
		if first == "" {
			first = k
		}
		if k != first || !strings.HasPrefix(k, "ssh-ed25519 ") {
			t.Errorf("concurrent first calls disagree: %q and %q", first, k)
		}
	}
	if derived, _, code := run([]string{"HOME=/home/workharbor"}, "sh", "-c", "ssh-keygen -y -f /home/workharbor/.whr-sshd/host_ed25519"); code != 0 || !strings.HasPrefix(first, derived) {
		t.Errorf("the public host key %q is not the private key's (%q)", first, derived)
	}
	hostLine, errOut, code := run([]string{"HOME=/home/workharbor"}, "/usr/local/bin/whr-sshd", "hostkey")
	if code != 0 {
		t.Fatalf("hostkey: exit %d: %s", code, errOut)
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(hostLine))
	if err != nil {
		t.Fatalf("the host key %q: %v", hostLine, err)
	}
	if again, _, _ := run([]string{"HOME=/home/workharbor"}, "/usr/local/bin/whr-sshd", "hostkey"); again != hostLine {
		t.Error("the host key changed between two calls: the console would lose its identity")
	}

	_, clientPriv := genKey(t)
	clientSigner, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}
	signed := func(c *sshca.CA, principal string, forwarding bool) ssh.Signer {
		_, cert, err := c.Sign(clientSigner.PublicKey(), sshca.Request{Principal: principal, KeyID: "live", Forwarding: forwarding})
		if err != nil {
			t.Fatal(err)
		}
		s, err := ssh.NewCertSigner(cert, clientSigner)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	connect := func(auth ssh.AuthMethod, hk ssh.PublicKey) (*ssh.Client, *execConn, error) {
		pr, pw := io.Pipe()
		cctx, ccancel := context.WithCancel(ctx)
		env := []string{"HOME=/home/workharbor", "WHR_SSH_CA=" + ca.PublicKey(), "HTTPS_PROXY=http://192.0.2.1:3128", "NO_PROXY=localhost"}
		st, err := h.Adapter.Exec(cctx, id, runtime.ExecRequest{Cmd: []string{"/usr/local/bin/whr-sshd"}, Env: env, Stdin: pr})
		if err != nil {
			ccancel()
			t.Fatal(err)
		}
		ec := &execConn{stdin: pw, st: st, cancel: ccancel}
		sc, chans, reqs, err := ssh.NewClientConn(ec, "whr-console", &ssh.ClientConfig{
			User: "workharbor", Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.FixedHostKey(hk), Timeout: 30 * time.Second,
		})
		if err != nil {
			_ = ec.Close()
			return nil, ec, err
		}
		return ssh.NewClient(sc, chans, reqs), ec, nil
	}

	// A certificate of the authority for workharbor: a shell as workharbor, with the proxy
	// variables the supervisor passed on, in a read-only root.
	client, ec, err := connect(ssh.PublicKeys(signed(ca, "workharbor", false)), hostKey)
	if err != nil {
		t.Fatalf("a good certificate was refused: %v\n%s", err, ec.logTxt.String())
	}
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	outB, err := sess.CombinedOutput("id -un; echo $HOME; echo proxy=$HTTPS_PROXY; touch /usr/x 2>&1 | head -1")
	if err != nil {
		t.Fatalf("session: %v: %s", err, outB)
	}
	text := string(outB)
	for _, want := range []string{"workharbor\n", "/home/workharbor\n", "proxy=http://192.0.2.1:3128"} {
		if !strings.Contains(text, want) {
			t.Errorf("the session lacks %q:\n%s", want, text)
		}
	}
	if !strings.Contains(strings.ToLower(text), "read-only") && !strings.Contains(strings.ToLower(text), "permission denied") {
		t.Errorf("the root is writable:\n%s", text)
	}
	// Forwarding was not granted: a forward to the console's own loopback is refused,
	// and so is one to another host.
	if c, err := client.Dial("tcp", "127.0.0.1:22"); err == nil {
		_ = c.Close()
		t.Error("a port was forwarded with a certificate that does not allow it")
	}
	_ = client.Close()

	// With the permission, loopback only.
	fwd, ec2, err := connect(ssh.PublicKeys(signed(ca, "workharbor", true)), hostKey)
	if err != nil {
		t.Fatalf("a certificate with forwarding: %v\n%s", err, ec2.logTxt.String())
	}
	if c, err := fwd.Dial("tcp", "192.0.2.7:80"); err == nil {
		_ = c.Close()
		t.Error("a forward to another host was allowed")
	}
	_ = fwd.Close()

	for name, tc := range map[string]struct {
		auth ssh.AuthMethod
		hk   ssh.PublicKey
	}{
		"a certificate of another authority": {ssh.PublicKeys(signed(other, "workharbor", false)), hostKey},
		"another principal":                  {ssh.PublicKeys(signed(ca, "other", false)), hostKey},
		"a bare key without a certificate":   {ssh.PublicKeys(clientSigner), hostKey},
		"a password":                         {ssh.Password("whr"), hostKey},
	} {
		bad, ec, err := connect(tc.auth, tc.hk)
		if err == nil {
			_ = bad.Close()
			t.Errorf("%s got in", name)
			continue
		}
		var ne *net.OpError
		if errors.As(err, &ne) {
			t.Errorf("%s: %v (not an authentication failure)", name, err)
		}
		_ = ec.Close()
	}

	// A client that does not know the host key refuses the console.
	_, wrongHost := genKey(t)
	wrongSigner, _ := ssh.NewSignerFromKey(wrongHost)
	if bad, _, err := connect(ssh.PublicKeys(signed(ca, "workharbor", false)), wrongSigner.PublicKey()); err == nil {
		_ = bad.Close()
		t.Error("a console with another host key was trusted")
	}
}

func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}
