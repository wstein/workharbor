package api

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/termproto"
)

// pipeSSH is an sshd that echoes in upper case and notes that it was closed.
type pipeSSH struct {
	mu     sync.Mutex
	in     *io.PipeReader
	inW    *io.PipeWriter
	out    *io.PipeReader
	outW   *io.PipeWriter
	closed chan struct{}
	once   sync.Once
	got    bytes.Buffer
}

func newPipeSSH() *pipeSSH {
	ir, iw := io.Pipe()
	or, ow := io.Pipe()
	p := &pipeSSH{in: ir, inW: iw, out: or, outW: ow, closed: make(chan struct{})}
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ir.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.got.Write(buf[:n])
				p.mu.Unlock()
				_, _ = ow.Write(bytes.ToUpper(buf[:n]))
			}
			if err != nil {
				_ = ow.Close()
				return
			}
		}
	}()
	return p
}

func (p *pipeSSH) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *pipeSSH) Write(b []byte) (int, error) { return p.inW.Write(b) }
func (p *pipeSSH) Close() error {
	p.once.Do(func() { _ = p.inW.Close(); _ = p.outW.Close(); close(p.closed) })
	return nil
}

var sshHeaders = []string{"Connection: Upgrade", "Upgrade: " + termproto.SSHUpgrade}

func TestTheSSHStreamIsBehindTheTokenAndNeedsTheUpgrade(t *testing.T) {
	r := newRig(t)
	r.be.onSSH = func() (service.SSHConn, error) { return newPipeSSH(), nil }
	if _, _, status, _ := r.upgradeTo("/v1/console/ssh", "", false, sshHeaders...); status != http.StatusUnauthorized || r.be.sshAsked != 0 {
		t.Errorf("without the token: %d, asked %d", status, r.be.sshAsked)
	}
	for name, hdr := range map[string][]string{
		"no upgrade": nil, "wrong protocol": {"Connection: Upgrade", "Upgrade: websocket"},
		"the terminal's protocol": {"Connection: Upgrade", "Upgrade: " + termproto.Upgrade},
		"no connection header":    {"Upgrade: " + termproto.SSHUpgrade},
	} {
		if _, _, status, _ := r.upgradeTo("/v1/console/ssh", "", true, hdr...); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, status)
		}
	}
	if r.be.sshAsked != 0 {
		t.Errorf("a refused request reached the console: %d", r.be.sshAsked)
	}
	r.be.onSSH = nil
	if _, _, status, _ := r.upgradeTo("/v1/console/ssh", "", true, sshHeaders...); status != http.StatusConflict {
		t.Errorf("a console that is not open: %d, want 409", status)
	}
}

func TestAnSSHStreamCarriesRawBytesBothWaysAndEndsWithTheClient(t *testing.T) {
	r := newRig(t)
	p := newPipeSSH()
	r.be.onSSH = func() (service.SSHConn, error) { return p, nil }
	conn, br, status, hdr := r.upgradeTo("/v1/console/ssh", "", true, sshHeaders...)
	if status != http.StatusSwitchingProtocols || !strings.EqualFold(hdr.Get("Upgrade"), termproto.SSHUpgrade) {
		t.Fatalf("status %d, upgrade %q", status, hdr.Get("Upgrade"))
	}
	if _, err := io.WriteString(conn, "SSH-2.0-OpenSSH_9.9\r\n\x00\x01binary"); err != nil {
		t.Fatal(err)
	}
	want := strings.ToUpper("SSH-2.0-OpenSSH_9.9\r\n\x00\x01binary")
	got := make([]byte, len(want))
	if _, err := io.ReadFull(br, got); err != nil || string(got) != want {
		t.Fatalf("read %q, %v", got, err)
	}
	_ = conn.Close() // the client leaves
	select {
	case <-p.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("sshd was not ended when the client left")
	}
}

func TestACertificateIsRequestedWithAPublicKeyOnly(t *testing.T) {
	r := newRig(t)
	status, _, body := r.do("POST", "/v1/console/ssh/certificate", `{"public_key":"ssh-ed25519 AAAAkey me","forwarding":true}`)
	if status != http.StatusOK || !strings.Contains(body, `"principal":"whr"`) || !strings.Contains(body, `"host_key":"ssh-ed25519 AAAAhost"`) {
		t.Fatalf("status %d, body %s", status, body)
	}
	r.be.mu.Lock()
	asked := r.be.sshCerts
	r.be.mu.Unlock()
	if len(asked) != 1 || asked[0].PublicKey != "ssh-ed25519 AAAAkey me" || !asked[0].Forwarding || !strings.HasPrefix(asked[0].Actor, "api:") {
		t.Errorf("asked = %+v", asked)
	}
	for name, body := range map[string]string{"empty": `{}`, "too long": `{"public_key":"` + strings.Repeat("a", 5000) + `"}`, "not json": `x`, "unknown field": `{"public_key":"k","private_key":"p"}`} {
		if status, _, _ := r.do("POST", "/v1/console/ssh/certificate", body); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, status)
		}
	}
}
