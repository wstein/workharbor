package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/sshca"
)

const testHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIH4HjX3W0zH6zN8VQe3a9nB1Kq2cT4mVxYfE7pLrUoZk whr-console"

// sshConsoles gives the rig a console with an SSH authority, whose sshd is `cat`
// (it echoes the connection) and whose host key the fake runtime answers.
func (r *wsRig) sshConsoles() (*Consoles, *sshca.CA) {
	r.t.Helper()
	path := filepath.Join(r.t.TempDir(), "ca")
	if err := sshca.Generate(path); err != nil {
		r.t.Fatal(err)
	}
	ca, err := sshca.Load(path)
	if err != nil {
		r.t.Fatal(err)
	}
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if slices.Equal(cmd, []string{"cat", "hostkey"}) {
			return []byte(testHostKey + "\n"), "", 0, true
		}
		return nil, "", 0, false
	}
	c := NewConsoles(r.svc, ConsoleConfig{
		Spec: func([]domain.Workspace) runtime.Spec {
			spec := r.rt.NewSpec()
			spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountVolume, Source: "whtmp-conformance-console-home", Target: "/home/whr"})
			return spec
		},
		Prepare: r.rt.Prepare,
		SSH:     ca,
		SSHCmd:  []string{"cat"},
	})
	return c, ca
}

func clientPub(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
}

func TestWithoutAnAuthorityThereIsNoSSH(t *testing.T) {
	r := newWsRig(t)
	c := r.consoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SSHCertificate(bg, SSHRequest{PublicKey: clientPub(t), Actor: "werner"}); !errors.Is(err, ErrNoSSH) {
		t.Errorf("certificate: %v", err)
	}
	if _, err := c.SSH(bg, "werner"); !errors.Is(err, ErrNoSSH) {
		t.Errorf("connection: %v", err)
	}
}

func TestSSHNeedsTheConsoleOpen(t *testing.T) {
	r := newWsRig(t)
	c, _ := r.sshConsoles()
	var ce *domain.ConflictError
	if _, err := c.SSHCertificate(bg, SSHRequest{PublicKey: clientPub(t), Actor: "werner"}); !errors.As(err, &ce) {
		t.Errorf("certificate without a console: %v", err)
	}
	if _, err := c.SSH(bg, "werner"); !errors.As(err, &ce) {
		t.Errorf("connection without a console: %v", err)
	}
}

func TestACertificateIsIssuedForTheClientsKeyAndAudited(t *testing.T) {
	r := newWsRig(t)
	c, ca := r.sshConsoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	got, err := c.SSHCertificate(bg, SSHRequest{PublicKey: clientPub(t), Actor: "wer ner\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Principal != "whr" || got.HostKey != testHostKey {
		t.Errorf("got %+v", got)
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(got.Certificate))
	if err != nil {
		t.Fatal(err)
	}
	cert, ok := parsed.(*ssh.Certificate)
	if !ok || cert.CertType != ssh.UserCert || len(cert.ValidPrincipals) != 1 || cert.ValidPrincipals[0] != "whr" {
		t.Fatalf("certificate = %#v", parsed)
	}
	if caKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(ca.PublicKey())); err != nil || string(cert.SignatureKey.Marshal()) != string(caKey.Marshal()) {
		t.Errorf("the certificate is not signed by the authority: %v", err)
	}
	if !strings.HasPrefix(cert.KeyId, "whr-werner-") {
		t.Errorf("key id = %q", cert.KeyId)
	}
	if _, has := cert.Extensions["permit-port-forwarding"]; has {
		t.Error("forwarding was not asked for")
	}
	if left := time.Until(got.ExpiresAt); left > sshca.DefaultTTL+time.Minute {
		t.Errorf("expires in %s", left)
	}
	evs, err := r.store.EventsSince(bg, domain.SupervisorStream, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evs {
		if e.Kind != domain.EventConsoleSSH {
			continue
		}
		found = true
		var p domain.ConsoleSSH
		if e.Tier != domain.TierAudit || json.Unmarshal(e.Payload, &p) != nil || p.Action != "certificate" || p.KeyID != cert.KeyId || p.Serial != cert.Serial {
			t.Errorf("audit = %+v, payload %s", e, e.Payload)
		}
		if strings.Contains(string(e.Payload), "ssh-ed25519") {
			t.Error("the audit entry holds a key")
		}
	}
	if !found {
		t.Fatalf("no audit entry in %+v", evs)
	}
	withFwd, err := c.SSHCertificate(bg, SSHRequest{PublicKey: clientPub(t), Forwarding: true, Actor: "werner"})
	if err != nil {
		t.Fatal(err)
	}
	p2, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(withFwd.Certificate))
	if _, has := p2.(*ssh.Certificate).Extensions["permit-port-forwarding"]; !has {
		t.Error("forwarding was asked for and not granted")
	}
}

func TestACertificateIsRefusedForWhatIsNotOnePublicKey(t *testing.T) {
	r := newWsRig(t)
	c, _ := r.sshConsoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	good := clientPub(t)
	for name, key := range map[string]string{
		"empty":     "",
		"garbage":   "not a key",
		"two keys":  good + "\n" + clientPub(t),
		"a private": "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----",
	} {
		_, err := c.SSHCertificate(bg, SSHRequest{PublicKey: key, Actor: "werner"})
		var ie *domain.InvalidError
		if !errors.As(err, &ie) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAnSSHConnectionCarriesBytesBothWaysAndEndsOnClose(t *testing.T) {
	r := newWsRig(t)
	c, ca := r.sshConsoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	conn, err := c.SSH(bg, "werner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("SSH-2.0-test\r\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "SSH-2.0-test\r\n" {
		t.Fatalf("read %q, %v", buf[:n], err)
	}
	// The launcher got the authority's public key and nothing secret.
	execs := r.fake.Execs()
	last := execs[len(execs)-1]
	if !slices.Contains(last.Req.Env, "WHR_SSH_CA="+ca.PublicKey()) || !slices.Contains(last.Req.Env, "HOME=/home/whr") {
		t.Errorf("env = %v", last.Req.Env)
	}
	for _, e := range last.Req.Env {
		if strings.Contains(e, "PRIVATE") {
			t.Errorf("a private key in the environment: %s", e)
		}
	}
	_ = conn.Close()
	if _, err := io.ReadAll(conn); err != nil {
		t.Errorf("read after close: %v", err)
	}
	eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.sshConns == 0 && len(c.openSSH) == 0
	})
}

func TestTheConsoleServesAtMostEightSSHConnections(t *testing.T) {
	r := newWsRig(t)
	c, _ := r.sshConsoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	var conns []SSHConn
	for range maxSSH {
		conn, err := c.SSH(bg, "werner")
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
	}
	if _, err := c.SSH(bg, "werner"); err == nil {
		t.Fatal("a ninth connection was served")
	}
	_ = conns[0].Close()
	eventually(t, func() bool {
		conn, err := c.SSH(bg, "werner")
		if err == nil {
			conns = append(conns, conn)
		}
		return err == nil
	})
	c.CloseSSH()
	eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.sshConns == 0
	})
}
