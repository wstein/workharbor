// Package sshca is the user certificate authority of the console's SSH access
// (design D43, §7.6, issue #32). The console's sshd trusts this authority's
// public key and nothing else: no password, no authorized_keys. Access is a
// certificate the supervisor signs for one session, for a few minutes, for one
// principal.
//
// The authority's private key is a secret file (0600, outside every root, read
// with config.ReadSecret). It is registered with the redactor and never leaves
// the supervisor: the guest gets only the public key.
package sshca

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/redact"
)

// Limits of a certificate.
const (
	DefaultTTL = 10 * time.Minute
	MaxTTL     = time.Hour
	// skew is how far a certificate's validity starts in the past, so a client
	// whose clock runs slightly behind the supervisor's still gets in.
	skew = time.Minute
	// minRSABits is the smallest RSA key a certificate is signed for.
	minRSABits = 3072
)

// principal is a user name worth putting in a certificate.
var principalRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// CA signs user certificates.
type CA struct {
	signer ssh.Signer
	pem    string // the private key's text, to register with the redactor
	now    func() time.Time
}

// Generate writes a new Ed25519 authority key to path: created exclusively with
// mode 0600, never overwritten, and its directory must exist. It is how the
// setup makes the key; the key is never printed.
func Generate(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("sshca: %q must be an absolute, clean path", path)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(priv, "whr-ssh-ca")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the operator names the file; created exclusively
	if err != nil {
		return fmt.Errorf("sshca: create the authority key: %w", err)
	}
	if _, err := f.Write(pem.EncodeToMemory(block)); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}

// Load reads the authority key from a secret file. The file is checked like any
// secret (a regular file, 0600, owned by the user, one link). An error names the
// path, never the content.
func Load(path string) (*CA, error) {
	raw, err := config.ReadSecret(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("sshca: the authority key %q has a passphrase: the supervisor cannot ask for it", path)
		}
		return nil, fmt.Errorf("sshca: %q is not a private key", path)
	}
	switch signer.PublicKey().Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
	default:
		return nil, fmt.Errorf("sshca: the authority key %q is a %s key: use Ed25519", path, signer.PublicKey().Type())
	}
	return &CA{signer: signer, pem: string(raw), now: time.Now}, nil
}

// Register adds the authority's private key to the redactor, so it never reaches
// a log, a transcript or an event if it leaks into one.
func (c *CA) Register(r *redact.Redactor) {
	r.Add(strings.TrimSpace(c.pem))
	// The body without the armour is what a partial copy would show.
	for _, line := range strings.Split(c.pem, "\n") {
		if len(line) >= redact.MinSecretLength && !strings.HasPrefix(line, "-----") {
			r.Add(line)
		}
	}
}

// PublicKey is the authority's public key as one authorized_keys line, the form
// sshd's TrustedUserCAKeys reads.
func (c *CA) PublicKey() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(c.signer.PublicKey())))
}

// Request is a certificate to sign.
type Request struct {
	// Principal is the user name the certificate allows to log in as.
	Principal string
	// KeyID names the certificate in sshd's log: the session or the task.
	KeyID string
	// TTL is how long the certificate is valid, from now. Zero means DefaultTTL.
	TTL time.Duration
	// Forwarding allows port forwarding, which the editors' remote modes need.
	// Without it the certificate allows a terminal and nothing else.
	Forwarding bool
}

// Sign signs the client's public key into a certificate for this session and
// returns it in the authorized_keys form (what a *-cert.pub file holds). The
// client keeps its private key; the supervisor never sees it.
func (c *CA) Sign(pub ssh.PublicKey, req Request) ([]byte, *ssh.Certificate, error) {
	if _, isCert := pub.(*ssh.Certificate); isCert {
		return nil, nil, errors.New("sshca: a certificate cannot be signed again: send the public key")
	}
	if err := checkKey(pub); err != nil {
		return nil, nil, err
	}
	if !principalRe.MatchString(req.Principal) {
		return nil, nil, fmt.Errorf("sshca: %q is not a user name", req.Principal)
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	if ttl < 0 || ttl > MaxTTL {
		return nil, nil, fmt.Errorf("sshca: a certificate lasts %s at most", MaxTTL)
	}
	var serial [8]byte
	if _, err := rand.Read(serial[:]); err != nil {
		return nil, nil, err
	}
	now := c.now()
	ext := map[string]string{"permit-pty": ""}
	if req.Forwarding {
		ext["permit-port-forwarding"] = ""
	}
	cert := &ssh.Certificate{
		Key:             pub,
		Serial:          binary.BigEndian.Uint64(serial[:]),
		CertType:        ssh.UserCert,
		KeyId:           sanitizeKeyID(req.KeyID),
		ValidPrincipals: []string{req.Principal},
		ValidAfter:      uint64(now.Add(-skew).Unix()), //nolint:gosec // a time after 1970
		ValidBefore:     uint64(now.Add(ttl).Unix()),   //nolint:gosec // a time after 1970
		Permissions:     ssh.Permissions{Extensions: ext},
	}
	if err := cert.SignCert(rand.Reader, c.signer); err != nil {
		return nil, nil, fmt.Errorf("sshca: sign: %w", err)
	}
	return ssh.MarshalAuthorizedKey(cert), cert, nil
}

// checkKey refuses a client key that is too weak to sign.
func checkKey(pub ssh.PublicKey) error {
	switch pub.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoSKED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoSKECDSA256:
		return nil
	case ssh.KeyAlgoRSA:
		if cp, ok := pub.(ssh.CryptoPublicKey); ok {
			if k, ok := cp.CryptoPublicKey().(*rsa.PublicKey); ok && k.N.BitLen() >= minRSABits {
				return nil
			}
		}
		return fmt.Errorf("sshca: an RSA key needs at least %d bits", minRSABits)
	}
	return fmt.Errorf("sshca: a %s key is not accepted", pub.Type())
}

// sanitizeKeyID keeps a key ID to printable characters, since sshd logs it.
func sanitizeKeyID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x21 && r <= 0x7e && b.Len() < 64 {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "whr"
	}
	return b.String()
}
