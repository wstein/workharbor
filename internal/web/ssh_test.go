package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/service"
)

func TestConsolePublicKeyRefusesUnsafeInput(t *testing.T) {
	for _, key := range []string{"", strings.Join([]string{"-----BEGIN", "OPENSSH", "PRIVATE", "KEY-----"}, " "), strings.Repeat("x", 4097), "ssh-ed25519 bad", "ssh-ed25519 bad\nssh-ed25519 bad"} {
		if _, err := consolePublicKey(key); err == nil {
			t.Errorf("accepted invalid public key")
		}
	}
}

type certificateFake struct {
	*fake
	console string
	calls   int
	request service.SSHRequest
}

func (f *certificateFake) ConsoleStatus(context.Context) (*service.ConsoleInfo, error) {
	if f.console == "" {
		return nil, nil
	}
	return &service.ConsoleInfo{EnvID: f.console}, nil
}

func (f *certificateFake) ConsoleSSHCertificate(_ context.Context, req service.SSHRequest) (service.SSHCertificate, error) {
	f.calls++
	f.request = req
	return service.SSHCertificate{Certificate: "public certificate", HostKey: "public host key", Principal: service.SSHPrincipal, ExpiresAt: time.Now()}, nil
}

func TestConsoleCertificateRequiresBoundSingleUsePasskey(t *testing.T) {
	var opt Options
	p := newPKRigWith(t, nil, func(o *Options) { opt = *o })
	be := &certificateFake{fake: p.be, console: "console-one"}
	ui, err := New(be, opt)
	if err != nil {
		t.Fatal(err)
	}
	p.srv.Close()
	p.srv = httptest.NewServer(ui.Handler())
	t.Cleanup(p.srv.Close)
	p.enrol(p.browser())
	b := p.signIn()
	csrf := csrfOf(t, b)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	pk, _ := ssh.NewPublicKey(pub)
	key := string(ssh.MarshalAuthorizedKey(pk))
	begin := func(forward bool) ceremonyReply {
		body, _ := json.Marshal(map[string]any{"public_key": key, "forwarding": forward})
		resp, raw := b.postJSON("/console/ssh/begin", body, map[string]string{"X-CSRF-Token": csrf})
		if resp.StatusCode != 200 {
			t.Fatalf("begin: %d %s", resp.StatusCode, raw)
		}
		var cr ceremonyReply
		_ = json.Unmarshal(raw, &cr)
		return cr
	}
	finish := func(cr ceremonyReply) (*reply, []byte) {
		var a protocol.CredentialAssertion
		if err := json.Unmarshal(cr.Options, &a); err != nil {
			t.Fatal(err)
		}
		return b.postJSON("/console/ssh/finish?public_key=changed&forwarding=true&console=another", bodyOf(p.auth.Assert(&a, pkOrigin)), map[string]string{"X-CSRF-Token": csrf, "X-Ceremony": cr.Ceremony})
	}
	for _, headers := range []map[string]string{nil, {"X-CSRF-Token": csrf, "Origin": "https://evil.test"}, {"X-CSRF-Token": csrf, "Sec-Fetch-Site": "cross-site"}} {
		resp, _ := b.postJSON("/console/ssh/begin", []byte(`{}`), headers)
		if resp.StatusCode != 403 {
			t.Errorf("unauthorized begin: %d", resp.StatusCode)
		}
	}
	for _, bad := range []string{"private key", strings.Repeat("x", 4097), key + key, strings.Join([]string{"-----BEGIN", "OPENSSH", "PRIVATE", "KEY-----"}, " ") + "\n" + key} {
		body, _ := json.Marshal(map[string]string{"public_key": bad})
		resp, raw := b.postJSON("/console/ssh/begin", body, map[string]string{"X-CSRF-Token": csrf})
		if resp.StatusCode != 400 || strings.Contains(string(raw), bad) {
			t.Errorf("invalid input response %d", resp.StatusCode)
		}
	}
	// A different authenticated session cannot spend this session's ceremony.
	crOther := begin(false)
	other := p.signIn()
	var assertion protocol.CredentialAssertion
	_ = json.Unmarshal(crOther.Options, &assertion)
	respOther, _ := other.postJSON("/console/ssh/finish", bodyOf(p.auth.Assert(&assertion, pkOrigin)), map[string]string{"X-CSRF-Token": csrfOf(t, other), "X-Ceremony": crOther.Ceremony})
	if respOther.StatusCode != 403 || be.calls != 0 {
		t.Fatal("another session issued a certificate")
	}
	// An otherwise valid assertion for another action is refused.
	opts, cer, err := p.svc.StepUpBegin(bg, csrf, revokeBinding)
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(opts)
	wrong := ceremonyReply{Options: options, Ceremony: cer}
	respWrong, _ := finish(wrong)
	if respWrong.StatusCode != 403 || be.calls != 0 {
		t.Fatal("another action issued a certificate")
	}
	cr := begin(false)
	resp, raw := finish(cr)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(raw), "host_key") {
		t.Fatalf("finish: %d %s", resp.StatusCode, raw)
	}
	if be.calls != 1 || be.request.Forwarding || be.request.Actor != "web+passkey" || be.request.ExpectedConsole != "console-one" {
		t.Fatalf("request: %+v, calls %d", be.request, be.calls)
	}
	resp, _ = finish(cr)
	if resp.StatusCode != 403 || be.calls != 1 {
		t.Fatal("replay issued a certificate")
	}
	cr = begin(true)
	be.console = "console-two"
	resp, _ = finish(cr)
	if resp.StatusCode != 403 || be.calls != 1 {
		t.Fatal("replacement issued a certificate")
	}
	be.console = "console-one"
	cr = begin(true)
	resp, _ = finish(cr)
	if resp.StatusCode != 200 || !be.request.Forwarding || be.calls != 2 {
		t.Fatal("explicit forwarding not preserved")
	}
	be.console = ""
	resp, _ = b.postJSON("/console/ssh/begin", []byte(`{}`), map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != 409 {
		t.Fatal("absent console accepted")
	}
}

func TestConsoleCertificateUnavailableWithoutBackendOrPasskey(t *testing.T) {
	p := newPKRig(t)
	b := p.browser()
	b.signIn()
	csrf := csrfOf(t, b)
	resp, _ := b.postJSON("/console/ssh/begin", []byte(`{}`), map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != 403 {
		t.Fatalf("missing backend/passkey: %d", resp.StatusCode)
	}
	anon := p.browser()
	resp, _ = anon.postJSON("/console/ssh/begin", []byte(`{}`), nil)
	if resp.StatusCode != 401 {
		t.Fatalf("missing session: %d", resp.StatusCode)
	}
}

func TestConsolePublicKeyCanonicalizesAndRejectsCertificates(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ssh.NewPublicKey(pub)
	signer, _ := ssh.NewSignerFromKey(private)
	cert := &ssh.Certificate{Key: key, CertType: ssh.UserCert, ValidPrincipals: []string{service.SSHPrincipal}}
	if err := cert.SignCert(rand.Reader, signer); err != nil {
		t.Fatal(err)
	}
	if _, err := consolePublicKey(string(ssh.MarshalAuthorizedKey(cert))); err == nil {
		t.Fatal("certificate accepted as public key")
	}
	canonical := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	got, err := consolePublicKey(canonical + " device comment\n")
	if err != nil || got != canonical {
		t.Fatalf("canonicalization: %q %v", got, err)
	}
}
