package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/sshca"
)

// SSHPrincipal is the one user a console certificate is valid for: the console's
// own user.
const SSHPrincipal = "workharbor"

// sshLauncher is the in-guest program that runs sshd for one connection (package
// console, whr-sshd).
const sshLauncher = "/usr/local/bin/whr-sshd"

// maxSSH is how many SSH connections the console serves at once.
const maxSSH = 8

// ErrNoSSH is what the SSH operations answer when the supervisor has no
// certificate authority configured.
var ErrNoSSH = domain.NewConflict(domain.RuleEnvRunning, "SSH access is not set up: the supervisor has no certificate authority (console.ssh_ca_key_file)")

// SSHRequest is a certificate to issue for a client's key.
type SSHRequest struct {
	ExpectedConsole string // optional environment identity approved by the web assertion
	PublicKey       string // the client's public key, one authorized_keys line
	Forwarding      bool   // allow port forwarding, which the editors' remote modes need
	Actor           string // who asked, for the audit entry
}

// SSHCertificate is what the client needs to connect: the certificate for its key
// and the console's host key, which it pins instead of trusting the first sight.
type SSHCertificate struct {
	Certificate string    `json:"certificate"` // one authorized_keys line
	HostKey     string    `json:"host_key"`    // the console's host public key, one line
	Principal   string    `json:"principal"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// SSHCertificate signs the client's key for one session, if the console is open.
// The private key stays with the client. Every certificate is audited.
func (c *Consoles) SSHCertificate(ctx context.Context, req SSHRequest) (SSHCertificate, error) {
	if c.cfg.SSH == nil {
		return SSHCertificate{}, ErrNoSSH
	}
	cur, err := c.running(ctx)
	if err != nil {
		return SSHCertificate{}, err
	}
	if req.ExpectedConsole != "" && req.ExpectedConsole != cur.ID {
		return SSHCertificate{}, domain.NewConflict(domain.RuleEnvRunning, "the console changed: request a new certificate")
	}
	pub, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(req.PublicKey))
	if err != nil || len(strings.TrimSpace(string(rest))) != 0 {
		return SSHCertificate{}, &domain.InvalidError{Msg: "public_key is not one public key line"}
	}
	hostKey, err := c.sshHostKey(ctx, cur)
	if err != nil {
		return SSHCertificate{}, err
	}
	now := c.svc.clock.Now()
	keyID := fmt.Sprintf("whr-%s-%d", sanitizeActor(req.Actor), now.Unix())
	line, cert, err := c.cfg.SSH.Sign(pub, sshca.Request{Principal: SSHPrincipal, KeyID: keyID, Forwarding: req.Forwarding})
	if err != nil {
		return SSHCertificate{}, &domain.InvalidError{Msg: err.Error()}
	}
	expires := time.Unix(int64(cert.ValidBefore), 0).UTC() //nolint:gosec // a signing time
	c.auditSSH(ctx, domain.ConsoleSSH{Action: "certificate", Actor: req.Actor, KeyID: cert.KeyId, Serial: cert.Serial, Forwarding: req.Forwarding, ExpiresAt: expires}, now)
	return SSHCertificate{
		Certificate: strings.TrimSpace(string(line)), HostKey: hostKey, Principal: SSHPrincipal, ExpiresAt: expires,
	}, nil
}

// running returns the console, which must be open and running.
func (c *Consoles) running(ctx context.Context) (*runtime.Info, error) {
	cur, err := c.current(ctx)
	if err != nil {
		return nil, err
	}
	if cur == nil || cur.State != domain.EnvRunning {
		return nil, domain.NewConflict(domain.RuleEnvRunning, "the console is not open: open it first")
	}
	return cur, nil
}

// sshHostKey asks the console for its host public key, which it makes on first use
// and keeps in its home volume.
func (c *Consoles) sshHostKey(ctx context.Context, cur *runtime.Info) (string, error) {
	st, err := c.svc.rt.Exec(ctx, cur.ID, runtime.ExecRequest{Cmd: append(c.sshCmd(), "hostkey")})
	if err != nil {
		return "", fmt.Errorf("ask the console for its host key: %w", err)
	}
	out, errOut, code, err := runtime.Collect(st)
	if err != nil || code != 0 {
		return "", fmt.Errorf("the console's host key: exit %d: %s", code, tail(errOut))
	}
	key := strings.TrimSpace(string(out))
	pub, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(key))
	if err != nil || len(strings.TrimSpace(string(rest))) != 0 || pub.Type() != ssh.KeyAlgoED25519 {
		return "", errors.New("the console's host key is not one Ed25519 public key")
	}
	return key, nil
}

func (c *Consoles) sshCmd() []string {
	if len(c.cfg.SSHCmd) > 0 {
		return append([]string(nil), c.cfg.SSHCmd...)
	}
	return []string{sshLauncher}
}

// SSHConn is one SSH connection to the console: the bytes of an SSH session, to
// and from the sshd that runs for it in the console.
type SSHConn interface {
	io.ReadWriteCloser
}

// SSH starts sshd for one connection in the console and returns its stream. The
// client speaks the SSH protocol over it and must hold a certificate (SSHCertificate)
// the console's sshd accepts. Closing the connection ends sshd. Each connection is
// audited.
func (c *Consoles) SSH(ctx context.Context, actor string) (SSHConn, error) {
	if c.cfg.SSH == nil {
		return nil, ErrNoSSH
	}
	cur, err := c.running(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.sshConns >= maxSSH {
		c.mu.Unlock()
		return nil, domain.NewConflict(domain.RuleEnvRunning, "the console already serves %d SSH connections: close one first", maxSSH)
	}
	c.sshConns++
	c.mu.Unlock()
	release := func() {
		c.mu.Lock()
		c.sshConns--
		c.mu.Unlock()
	}
	env := append([]string{"HOME=/home/workharbor", "WHR_SSH_CA=" + c.cfg.SSH.PublicKey()}, c.svc.agentEnv(ctx, domain.ID(cur.ID))...)
	pr, pw := io.Pipe()
	// The command outlives the request that started it only as far as the caller
	// keeps the connection: Close cancels it.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	st, err := c.svc.rt.Exec(runCtx, cur.ID, runtime.ExecRequest{Cmd: c.sshCmd(), Env: env, Stdin: pr})
	if err != nil {
		cancel()
		_ = pr.Close()
		release()
		return nil, err
	}
	c.auditSSH(ctx, domain.ConsoleSSH{Action: "connection", Actor: actor}, c.svc.clock.Now())
	conn := &sshConn{stdin: pw, pr: pr, st: st, cancel: cancel, release: release, report: c.svc.report}
	c.mu.Lock()
	if c.openSSH == nil {
		c.openSSH = map[*sshConn]struct{}{}
	}
	c.openSSH[conn] = struct{}{}
	c.mu.Unlock()
	conn.untrack = func() {
		c.mu.Lock()
		delete(c.openSSH, conn)
		c.mu.Unlock()
	}
	return conn, nil
}

// sshConn adapts an exec stream: reads are sshd's standard output, writes are its
// standard input. Its standard error (sshd's log, with -e) is kept, the tail of
// it, to say why a connection ended badly.
type sshConn struct {
	stdin   *io.PipeWriter
	pr      *io.PipeReader
	st      runtime.ExecStream
	cancel  context.CancelFunc
	release func()
	untrack func()
	report  func(error)

	mu     sync.Mutex
	rest   []byte // output read from the stream and not yet returned
	errLog []byte // the tail of sshd's log
	closed sync.Once
}

const sshLogTail = 4 << 10

func (c *sshConn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if len(c.rest) > 0 {
			n := copy(p, c.rest)
			c.rest = c.rest[n:]
			c.mu.Unlock()
			return n, nil
		}
		c.mu.Unlock()
		chunk, ok := <-c.st.Chunks()
		if !ok {
			return 0, io.EOF
		}
		c.mu.Lock()
		switch chunk.Stream {
		case runtime.Stdout:
			c.rest = append(c.rest, chunk.Data...)
		case runtime.Stderr:
			c.errLog = append(c.errLog, chunk.Data...)
			if len(c.errLog) > sshLogTail {
				c.errLog = c.errLog[len(c.errLog)-sshLogTail:]
			}
		}
		c.mu.Unlock()
	}
}

func (c *sshConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

// Close ends the connection: sshd gets end of input and, if it does not end, is
// cancelled. A bad exit is reported with the tail of its log.
func (c *sshConn) Close() error {
	c.closed.Do(func() {
		_ = c.stdin.Close()
		_ = c.pr.Close()
		c.cancel()
		go func() {
			// Drain, so the stream can finish, then look at how it ended.
			for range c.st.Chunks() {
			}
			code, _ := c.st.Wait()
			c.untrack()
			c.release()
			if code != 0 && code != 130 && code != 255 {
				c.mu.Lock()
				log := string(c.errLog)
				c.mu.Unlock()
				c.report(fmt.Errorf("the console's sshd ended with exit %d: %s", code, strings.TrimSpace(log)))
			}
		}()
	})
	return nil
}

// CloseSSH ends every SSH connection that is open: the supervisor is stopping.
func (c *Consoles) CloseSSH() {
	c.mu.Lock()
	conns := make([]*sshConn, 0, len(c.openSSH))
	for s := range c.openSSH {
		conns = append(conns, s)
	}
	c.mu.Unlock()
	for _, s := range conns {
		_ = s.Close()
	}
}

func (c *Consoles) auditSSH(ctx context.Context, e domain.ConsoleSSH, at time.Time) {
	saved, err := c.svc.store.Append(ctx, domain.NewConsoleSSHEvent(e, at))
	if err != nil {
		c.svc.report(fmt.Errorf("audit the console ssh %s: %w", e.Action, err))
		return
	}
	c.svc.publish(saved)
}

// sanitizeActor keeps an actor name to characters that are safe in a certificate's key ID.
func sanitizeActor(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 24 {
			break
		}
	}
	if b.Len() == 0 {
		return "api"
	}
	return b.String()
}

// tail returns the last bytes of a log as text.
func tail(b []byte) string {
	if len(b) > 512 {
		b = b[len(b)-512:]
	}
	return strings.TrimSpace(string(b))
}
