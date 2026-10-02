package sshca

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/gittest"
	"github.com/wstein/workharbor/internal/redact"
)

func newCA(t *testing.T) (*CA, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca")
	if err := Generate(path); err != nil {
		t.Fatal(err)
	}
	ca, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return ca, path
}

func clientKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestGenerateCreatesAPrivateFileOnceAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca")
	if err := Generate(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, err %v", info.Mode(), err)
	}
	before, _ := os.ReadFile(path) //nolint:gosec // a key file this test made
	if err := Generate(path); err == nil {
		t.Fatal("an existing key was overwritten")
	}
	after, _ := os.ReadFile(path) //nolint:gosec // a key file this test made
	if string(after) != string(before) {
		t.Fatal("the key changed")
	}
	if Generate("relative/ca") == nil {
		t.Error("a relative path was accepted")
	}
}

func TestLoadRefusesWhatIsNotASafeKey(t *testing.T) {
	dir := t.TempDir()
	_, path := newCA(t)
	loose := filepath.Join(dir, "loose")
	raw, _ := os.ReadFile(path)                             //nolint:gosec // a key file this test made
	if err := os.WriteFile(loose, raw, 0o644); err != nil { //nolint:gosec // the test makes a file that is too open on purpose
		t.Fatal(err)
	}
	if _, err := Load(loose); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Errorf("a world-readable key: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil {
		t.Error("a link was followed")
	}
	junk := filepath.Join(dir, "junk")
	if err := os.WriteFile(junk, []byte("not a key at all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(junk); err == nil || strings.Contains(err.Error(), "not a key at all") {
		t.Errorf("junk: %v", err)
	}
	// An RSA authority is refused: the authority is Ed25519 or ECDSA.
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	block, _ := ssh.MarshalPrivateKey(rk, "")
	rsaPath := filepath.Join(dir, "rsa")
	if err := os.WriteFile(rsaPath, []byte(pemString(block)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(rsaPath); err == nil || !strings.Contains(err.Error(), "Ed25519") {
		t.Errorf("an RSA authority: %v", err)
	}
	pass, err := ssh.MarshalPrivateKeyWithPassphrase(mustEd(t), "", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	passPath := filepath.Join(dir, "pass")
	if err := os.WriteFile(passPath, []byte(pemString(pass)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(passPath); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("a key with a passphrase: %v", err)
	}
}

func TestACertificateIsShortLivedPerSessionAndForOnePrincipal(t *testing.T) {
	ca, _ := newCA(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ca.now = func() time.Time { return now }
	pub := clientKey(t)
	line, cert, err := ca.Sign(pub, Request{Principal: "whr", KeyID: "session 1\n\x00", TTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey(line)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := parsed.(*ssh.Certificate)
	if !ok || c.CertType != ssh.UserCert || len(c.ValidPrincipals) != 1 || c.ValidPrincipals[0] != "whr" {
		t.Fatalf("certificate = %#v", parsed)
	}
	if got := time.Unix(int64(c.ValidBefore), 0).Sub(now); got != 5*time.Minute { //nolint:gosec // a test time
		t.Errorf("valid for %s", got)
	}
	if time.Unix(int64(c.ValidAfter), 0).After(now) { //nolint:gosec // a test time
		t.Error("a certificate that is not yet valid at signing time")
	}
	if c.KeyId != "session1" {
		t.Errorf("key id = %q", c.KeyId)
	}
	if _, has := c.Extensions["permit-port-forwarding"]; has {
		t.Error("port forwarding is allowed by default")
	}
	if _, has := c.Extensions["permit-pty"]; !has || len(c.CriticalOptions) != 0 {
		t.Errorf("extensions %v, options %v", c.Extensions, c.CriticalOptions)
	}
	if cert.Serial == 0 {
		t.Log("a zero serial is possible but not expected")
	}
	// The checker sshd uses: trusted authority, principal, time.
	checker := &ssh.CertChecker{Clock: func() time.Time { return now.Add(time.Minute) }, IsUserAuthority: func(a ssh.PublicKey) bool { return string(a.Marshal()) == string(ca.signer.PublicKey().Marshal()) }}
	if err := checker.CheckCert("whr", c); err != nil {
		t.Errorf("a good certificate is refused: %v", err)
	}
	if err := checker.CheckCert("root", c); err == nil {
		t.Error("another principal was accepted")
	}
	// CheckCert checks the principal, the time and the signature; whether the
	// signer is trusted is the authority callback, which sshd answers from
	// TrustedUserCAKeys.
	if !checker.IsUserAuthority(c.SignatureKey) {
		t.Error("the certificate is not signed by this authority")
	}
	other, _ := newCA(t)
	if string(c.SignatureKey.Marshal()) == string(other.signer.PublicKey().Marshal()) {
		t.Error("two authorities have the same key")
	}
	forged := *c
	forged.ValidBefore += 3600
	if err := checker.CheckCert("whr", &forged); err == nil {
		t.Error("a certificate with a longer life than was signed was accepted")
	}
}

func TestSignRefusesWhatItShouldNotCertify(t *testing.T) {
	ca, _ := newCA(t)
	pub := clientKey(t)
	_, cert, err := ca.Sign(pub, Request{Principal: "whr"})
	if err != nil {
		t.Fatal(err)
	}
	weak, _ := rsa.GenerateKey(rand.Reader, 2048)
	weakPub, _ := ssh.NewPublicKey(&weak.PublicKey)
	dsaLike, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecPub, _ := ssh.NewPublicKey(&dsaLike.PublicKey)
	cases := map[string]struct {
		pub ssh.PublicKey
		req Request
	}{
		"a certificate again": {cert, Request{Principal: "whr"}},
		"a weak RSA key":      {weakPub, Request{Principal: "whr"}},
		"root's name quoted":  {pub, Request{Principal: "wh r"}},
		"an empty principal":  {pub, Request{}},
		"an uppercase one":    {pub, Request{Principal: "Root"}},
		"too long a life":     {pub, Request{Principal: "whr", TTL: 2 * time.Hour}},
		"a negative life":     {pub, Request{Principal: "whr", TTL: -time.Minute}},
	}
	for name, tc := range cases {
		if _, _, err := ca.Sign(tc.pub, tc.req); err == nil {
			t.Errorf("%s was signed", name)
		}
	}
	if _, _, err := ca.Sign(ecPub, Request{Principal: "whr", Forwarding: true}); err != nil {
		t.Errorf("an ECDSA key with forwarding: %v", err)
	}
}

func TestForwardingIsOptIn(t *testing.T) {
	ca, _ := newCA(t)
	_, cert, err := ca.Sign(clientKey(t), Request{Principal: "whr", Forwarding: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cert.Extensions["permit-port-forwarding"]; !ok {
		t.Error("forwarding was asked for and not granted")
	}
}

func TestThePublicKeyIsAnAuthorizedKeysLineWithoutTheSecret(t *testing.T) {
	ca, path := newCA(t)
	pubLine := ca.PublicKey()
	if strings.Contains(pubLine, "\n") || !strings.HasPrefix(pubLine, "ssh-ed25519 ") {
		t.Fatalf("public key = %q", pubLine)
	}
	raw, _ := os.ReadFile(path) //nolint:gosec // a key file this test made
	for _, line := range strings.Split(string(raw), "\n") {
		if len(line) > 20 && !strings.HasPrefix(line, "-----") && strings.Contains(pubLine, line) {
			t.Error("the public key holds a line of the private one")
		}
	}
}

func TestTheRedactorKnowsTheAuthorityKey(t *testing.T) {
	ca, path := newCA(t)
	r := redact.New()
	ca.Register(r)
	raw, _ := os.ReadFile(path) //nolint:gosec // a key file this test made
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	body := lines[1]
	if got := r.String("leaked: " + body + " and more"); strings.Contains(got, body) {
		t.Errorf("a line of the key got through: %q", got)
	}
	if got := r.String(string(raw)); strings.Contains(got, body) {
		t.Errorf("the whole key got through: %q", got)
	}
}

// ssh-keygen, which is what sshd's own tooling is, reads the certificate.
func TestSSHKeygenReadsTheCertificate(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	ca, _ := newCA(t)
	line, _, err := ca.Sign(clientKey(t), Request{Principal: "whr", KeyID: "s1", TTL: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "id-cert.pub")
	if err := os.WriteFile(file, line, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := gittest.SSHKeygen(ctx, dir, "-L", "-f", file).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen -L: %v\n%s", err, out)
	}
	for _, want := range []string{"user certificate", "Key ID: \"s1\"", "whr", "permit-pty"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("ssh-keygen -L lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "port-forwarding") {
		t.Errorf("port forwarding is listed:\n%s", out)
	}
}

func pemString(b *pem.Block) string { return string(pem.EncodeToMemory(b)) }

func mustEd(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
