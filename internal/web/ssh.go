package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/wstein/workharbor/internal/passkey"
	"github.com/wstein/workharbor/internal/service"
)

// consoleCertificates is the existing service-backed console capability.
type consoleCertificates interface {
	ConsoleStatus(context.Context) (*service.ConsoleInfo, error)
	ConsoleSSHCertificate(context.Context, service.SSHRequest) (service.SSHCertificate, error)
}

const consoleSSHAction = "op:console-ssh-certificate"

// consoleSSHBinding lives only in the passkey service's bounded, expiring,
// session-bound single-use ceremony pool. No caller supplies it at finish.
type consoleSSHBinding struct {
	Console    string `json:"console"`
	Key        string `json:"key"`
	Digest     string `json:"digest"`
	Forwarding bool   `json:"forwarding"`
}

func consolePublicKey(line string) (string, error) {
	if len(line) > 4096 || strings.ContainsAny(strings.TrimSpace(line), "\r\n") {
		return "", errors.New("invalid public key")
	}
	pub, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil || len(options) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
		return "", errors.New("invalid public key")
	}
	if _, cert := pub.(*ssh.Certificate); cert {
		return "", errors.New("invalid public key")
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), nil
}

func keyDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *Server) consoleCertificatePage(w http.ResponseWriter, r *http.Request, sess Session) {
	_, available := s.be.(consoleCertificates)
	enrolled := false
	if s.opt.Passkeys != nil {
		enrolled, _ = s.opt.Passkeys.Enrolled(r.Context())
	}
	s.render(w, r, http.StatusOK, consoleCertificateView(consoleCertificatePage{nav: s.navOf(r, sess, "console"), Available: available && enrolled}))
}

type consoleCertificatePage struct {
	nav
	Available bool
}

func (s *Server) certificateBackend(w http.ResponseWriter, r *http.Request) (consoleCertificates, *service.ConsoleInfo, bool) {
	be, ok := s.be.(consoleCertificates)
	enrolled := false
	if s.opt.Passkeys != nil {
		enrolled, _ = s.opt.Passkeys.Enrolled(r.Context())
	}
	if !ok || !enrolled || !sameOrigin(r) {
		jsonError(w, http.StatusForbidden, "Console certificates need an enrolled passkey and a configured console.")
		return nil, nil, false
	}
	cur, err := be.ConsoleStatus(r.Context())
	if err != nil || cur == nil || cur.EnvID == "" {
		jsonError(w, http.StatusConflict, "Open the console on the host first.")
		return nil, nil, false
	}
	return be, cur, true
}

func (s *Server) consoleCertificateBegin(w http.ResponseWriter, r *http.Request, sess Session) {
	_, cur, ok := s.certificateBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		PublicKey  string `json:"public_key"`
		Forwarding bool   `json:"forwarding"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "Paste one public key of at most 4096 bytes.")
		return
	}
	key, err := consolePublicKey(in.PublicKey)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Paste one public key of at most 4096 bytes.")
		return
	}
	bound, _ := json.Marshal(consoleSSHBinding{Console: cur.EnvID, Key: key, Digest: keyDigest(key), Forwarding: in.Forwarding})
	s.beginStepUp(w, r, sess, passkey.Binding{Decision: consoleSSHAction, SHA: string(bound)})
}

func (s *Server) consoleCertificateFinish(w http.ResponseWriter, r *http.Request, sess Session) {
	be, cur, ok := s.certificateBackend(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, jsonLimit)
	got, err := s.opt.Passkeys.StepUpFinish(r.Context(), r.Header.Get("X-Ceremony"), sess.CSRF, r)
	if err != nil {
		jsonError(w, passkeyStatus(err, http.StatusForbidden), passkeyMessage(err))
		return
	}
	var bound consoleSSHBinding
	if got.Decision != consoleSSHAction || json.Unmarshal([]byte(got.SHA), &bound) != nil {
		jsonError(w, http.StatusForbidden, "That assertion was for another action.")
		return
	}
	key, err := consolePublicKey(bound.Key)
	expected, _ := json.Marshal(bound)
	if err != nil || key != bound.Key || keyDigest(key) != bound.Digest || bound.Console != cur.EnvID || string(expected) != got.SHA {
		jsonError(w, http.StatusForbidden, "The console or certificate request changed. Try again.")
		return
	}
	cert, err := be.ConsoleSSHCertificate(r.Context(), service.SSHRequest{PublicKey: key, Forwarding: bound.Forwarding, Actor: "web+passkey", ExpectedConsole: bound.Console})
	if err != nil {
		jsonError(w, http.StatusConflict, "The console certificate could not be issued. Check the console configuration on the host.")
		return
	}
	jsonReply(w, http.StatusOK, cert)
}
