// SPDX-License-Identifier: EUPL-1.2
// Package identity persists host-owned key possession outside workspaces.
package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
)

var ErrIdentity = errors.New("invalid_or_unsafe_identity")
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Material struct {
	IdentityID        string `json:"identity_id"`
	PrivateKey        string `json:"private_key"`
	Certificate       string `json:"certificate"`
	IssuerCertificate string `json:"issuer_certificate"`
}
type Pending struct {
	PrivateKey            string `json:"private_key"`
	CSR                   string `json:"csr"`
	Certificate           string `json:"certificate"`
	IssuerCertificate     string `json:"issuer_certificate"`
	IdentityID            string `json:"identity_id"`
	PredecessorIdentityID string `json:"predecessor_identity_id"`
}
type State struct {
	SchemaVersion int       `json:"schema_version"`
	Endpoint      string    `json:"endpoint"`
	ServerCAFile  string    `json:"server_ca_file"`
	OwnerID       string    `json:"owner_id"`
	HostID        string    `json:"host_id"`
	Active        *Material `json:"active"`
	Pending       *Pending  `json:"pending"`
}
type Response struct {
	SchemaVersion     int    `json:"schema_version"`
	OwnerID           string `json:"owner_id"`
	HostID            string `json:"host_id"`
	IdentityID        string `json:"identity_id"`
	Certificate       string `json:"certificate"`
	IssuerCertificate string `json:"issuer_certificate"`
	ExpiresAt         int64  `json:"expires_at"`
}

func endpointOK(value string) bool {
	u, e := url.Parse(value)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == "" && (u.Path == "" || u.Path == "/") && u.RawPath == ""
}
func Prepare(dir, endpoint, serverCA string) (*State, error) {
	if !endpointOK(endpoint) || noSymlinkAncestors(dir, true) != nil {
		return nil, ErrIdentity
	}
	if _, e := os.Lstat(filepath.Join(dir, "identity.json")); e == nil {
		return nil, ErrIdentity
	} else if !os.IsNotExist(e) {
		return nil, ErrIdentity
	}
	if serverCA != "" && !caPathOK(serverCA) {
		return nil, ErrIdentity
	}
	if e := os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
		return nil, ErrIdentity
	}
	// Sync even when the directory already exists: a retry after a failed sync
	// must not report success for an entry that may not be durable.
	if syncDir(filepath.Dir(dir)) != nil {
		return nil, ErrIdentity
	}
	if e := privatePath(dir, true); e != nil {
		return nil, e
	}
	pending, e := newPending("", "")
	if e != nil {
		return nil, e
	}
	state := &State{SchemaVersion: 1, Endpoint: strings.TrimSuffix(endpoint, "/"), ServerCAFile: serverCA, Pending: pending}
	if e = save(dir, state, true); e != nil {
		return nil, e
	}
	return state, nil
}
func Load(dir string) (*State, error) {
	if e := privatePath(dir, true); e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "identity.json")
	if e := privatePath(path, false); e != nil {
		return nil, e
	}
	data, e := os.ReadFile(path)
	if e != nil || len(data) > 32768 {
		return nil, ErrIdentity
	}
	native, parseErr := wirejson.Parse(data)
	if parseErr != nil || !stateShape(native) {
		return nil, ErrIdentity
	}
	var state State
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&state); e != nil {
		return nil, ErrIdentity
	}
	if e = validate(&state); e != nil {
		return nil, e
	}
	return &state, nil
}
func Save(dir string, state *State) error { return save(dir, state, false) }
func save(dir string, state *State, exclusive bool) error {
	if e := privatePath(dir, true); e != nil {
		return e
	}
	if e := validate(state); e != nil {
		return e
	}
	path := filepath.Join(dir, "identity.json")
	if !exclusive {
		if e := privatePath(path, false); e != nil {
			return e
		}
	}
	bytes, e := json.Marshal(state)
	if e != nil || len(bytes) > 32768 {
		return ErrIdentity
	}
	file, e := identityFiles.createTemp(dir, ".identity-")
	if e != nil {
		return ErrIdentity
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if e = file.Chmod(0600); e == nil {
		_, e = file.Write(bytes)
	}
	if e == nil {
		e = file.Sync()
	}
	closed := file.Close()
	if e == nil {
		e = closed
	}
	if e != nil {
		return ErrIdentity
	}
	if exclusive {
		e = identityFiles.link(temporary, path)
	} else {
		e = identityFiles.rename(temporary, path)
	}
	if e != nil {
		return ErrIdentity
	}
	if exclusive {
		// Drop the second name before the directory fsync so one sync makes the
		// link and the removal durable; a crash cannot leave a second key copy.
		if identityFiles.remove(temporary) != nil {
			return ErrIdentity
		}
	}
	return syncDir(dir)
}
func syncDir(dir string) error {
	parent, e := identityFiles.openDir(dir)
	if e != nil {
		return ErrIdentity
	}
	e = parent.Sync()
	closeErr := parent.Close()
	if e != nil || closeErr != nil {
		return ErrIdentity
	}
	return nil
}

// caPathOK requires an absolute, already-clean server CA path.
func caPathOK(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}
func privatePath(path string, directory bool) error {
	if noSymlinkAncestors(path, false) != nil {
		return ErrIdentity
	}
	st, e := os.Lstat(path)
	if e != nil {
		return ErrIdentity
	}
	want := os.FileMode(0600)
	if directory {
		want = 0700
	}
	unix, ok := st.Sys().(*syscall.Stat_t)
	if !ok || unix.Uid != uint32(os.Geteuid()) || st.Mode().Perm() != want || st.Mode()&os.ModeSymlink != 0 || st.IsDir() != directory || (!directory && !st.Mode().IsRegular()) || (!directory && st.Size() > 32768) {
		return ErrIdentity
	}
	return nil
}
func newPending(host, predecessor string) (*Pending, error) {
	_, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return nil, ErrIdentity
	}
	request := &x509.CertificateRequest{Subject: pkix.Name{CommonName: host}}
	if host != "" {
		raw, err := asn1.Marshal(pkix.RDNSequence{{pkix.AttributeTypeAndValue{Type: asn1.ObjectIdentifier{2, 5, 4, 3}, Value: asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte(host)}}}})
		if err != nil {
			return nil, ErrIdentity
		}
		request.RawSubject = raw
	}
	der, e := x509.CreateCertificateRequest(rand.Reader, request, key)
	if e != nil {
		return nil, ErrIdentity
	}
	encoded, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return nil, ErrIdentity
	}
	return &Pending{PrivateKey: base64.StdEncoding.EncodeToString(encoded), CSR: base64.StdEncoding.EncodeToString(der), PredecessorIdentityID: predecessor}, nil
}
func decode64(value string, max int) ([]byte, error) {
	if len(value) > 4*((max+2)/3) {
		return nil, ErrIdentity
	}
	b, e := base64.StdEncoding.DecodeString(value)
	if e != nil || len(b) > max || base64.StdEncoding.EncodeToString(b) != value {
		return nil, ErrIdentity
	}
	return b, nil
}
func privateKey(value string) (ed25519.PrivateKey, error) {
	der, e := decode64(value, 4096)
	if e != nil {
		return nil, e
	}
	key, e := x509.ParsePKCS8PrivateKey(der)
	if e != nil {
		return nil, ErrIdentity
	}
	ed, ok := key.(ed25519.PrivateKey)
	canonical, canonicalErr := x509.MarshalPKCS8PrivateKey(key)
	if !ok || canonicalErr != nil || !bytes.Equal(canonical, der) {
		return nil, ErrIdentity
	}
	return ed, nil
}
func certificate(value string) (*x509.Certificate, error) {
	b, e := decode64(value, 4096)
	if e != nil {
		return nil, e
	}
	c, e := x509.ParseCertificate(b)
	if e != nil {
		return nil, ErrIdentity
	}
	return c, nil
}
func material(m *Material, host string, now time.Time) (*x509.Certificate, error) {
	if m == nil || !uuid.MatchString(m.IdentityID) {
		return nil, ErrIdentity
	}
	key, e := privateKey(m.PrivateKey)
	if e != nil {
		return nil, e
	}
	cert, e := certificate(m.Certificate)
	if e != nil {
		return nil, e
	}
	issuer, e := certificate(m.IssuerCertificate)
	if e != nil {
		return nil, e
	}
	pub, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok || !pub.Equal(key.Public()) || cert.Subject.CommonName != host || len(cert.Subject.Names) != 1 || !cert.BasicConstraintsValid || len(cert.UnknownExtKeyUsage) != 0 || len(cert.UnhandledCriticalExtensions) != 0 || cert.IsCA || cert.KeyUsage != x509.KeyUsageDigitalSignature || len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth || cert.NotAfter.Sub(cert.NotBefore) != 24*time.Hour {
		return nil, ErrIdentity
	}
	if issuer.PublicKeyAlgorithm != x509.Ed25519 || issuer.MaxPathLen >= 0 || issuer.MaxPathLenZero || issuer.NotAfter.Sub(issuer.NotBefore) != 5*365*24*time.Hour || len(issuer.ExtKeyUsage) != 0 || len(issuer.UnknownExtKeyUsage) != 0 || len(issuer.UnhandledCriticalExtensions) != 0 || issuer.Subject.CommonName != "WorkHarbor client issuer" || len(issuer.Subject.Names) != 1 || !bytes.Equal(cert.RawIssuer, issuer.RawSubject) || !issuer.IsCA || !issuer.BasicConstraintsValid || issuer.KeyUsage != x509.KeyUsageCertSign || issuer.CheckSignatureFrom(issuer) != nil || cert.CheckSignatureFrom(issuer) != nil || cert.NotAfter.After(issuer.NotAfter) || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || now.Before(issuer.NotBefore) || !now.Before(issuer.NotAfter) {
		return nil, ErrIdentity
	}
	return cert, nil
}
func validate(s *State) error {
	if s == nil || s.SchemaVersion != 1 || !endpointOK(s.Endpoint) {
		return ErrIdentity
	}
	if s.ServerCAFile != "" && !caPathOK(s.ServerCAFile) {
		return ErrIdentity
	}
	if s.Active == nil {
		if s.OwnerID != "" || s.HostID != "" || s.Pending == nil || s.Pending.PredecessorIdentityID != "" || s.Pending.Certificate != "" || s.Pending.IssuerCertificate != "" || s.Pending.IdentityID != "" {
			return ErrIdentity
		}
	} else {
		if !uuid.MatchString(s.OwnerID) || !uuid.MatchString(s.HostID) {
			return ErrIdentity
		}
		// Persisted expired possession must remain loadable for explicit recovery;
		// transport and promotion separately validate current wall-clock validity.
		cert, e := certificate(s.Active.Certificate)
		if e != nil {
			return e
		}
		if _, e = material(s.Active, s.HostID, cert.NotBefore); e != nil {
			return e
		}
	}
	if p := s.Pending; p != nil {
		key, e := privateKey(p.PrivateKey)
		if e != nil {
			return e
		}
		bytes, e := decode64(p.CSR, 4096)
		if e != nil {
			return e
		}
		csr, e := x509.ParseCertificateRequest(bytes)
		if e != nil || csr.CheckSignature() != nil || len(csr.Extensions) != 0 || len(csr.Attributes) != 0 {
			return ErrIdentity
		}
		public, ok := csr.PublicKey.(ed25519.PublicKey)
		if !ok || !public.Equal(key.Public()) || csr.Subject.CommonName != s.HostID {
			return ErrIdentity
		}
		if s.Active != nil && p.PredecessorIdentityID != s.Active.IdentityID {
			return ErrIdentity
		}
		if p.Certificate != "" {
			if s.Active == nil {
				return ErrIdentity
			}
			c, e := certificate(p.Certificate)
			if e != nil {
				return e
			}
			if _, e = material(p.material(), s.HostID, c.NotBefore); e != nil {
				return e
			}
		} else if p.IssuerCertificate != "" || p.IdentityID != "" {
			return ErrIdentity
		}
	}
	return nil
}
func (p *Pending) material() *Material {
	return &Material{IdentityID: p.IdentityID, PrivateKey: p.PrivateKey, Certificate: p.Certificate, IssuerCertificate: p.IssuerCertificate}
}
func acceptResponse(s *State, r Response) (*Material, error) {
	if r.SchemaVersion != 1 || !uuid.MatchString(r.OwnerID) || !uuid.MatchString(r.HostID) || !uuid.MatchString(r.IdentityID) || s.Pending == nil {
		return nil, ErrIdentity
	}
	if s.Active != nil && (s.OwnerID != r.OwnerID || s.HostID != r.HostID) {
		return nil, ErrIdentity
	}
	m := &Material{IdentityID: r.IdentityID, PrivateKey: s.Pending.PrivateKey, Certificate: r.Certificate, IssuerCertificate: r.IssuerCertificate}
	c, e := material(m, r.HostID, time.Now())
	if e != nil || c.NotAfter.Unix() != r.ExpiresAt {
		return nil, ErrIdentity
	}
	return m, nil
}
func AcceptEnrollment(dir string, s *State, r Response) error {
	if s.Active != nil {
		return ErrIdentity
	}
	m, e := acceptResponse(s, r)
	if e != nil {
		return e
	}
	next := *s
	next.OwnerID = r.OwnerID
	next.HostID = r.HostID
	next.Active = m
	next.Pending = nil
	if e = Save(dir, &next); e != nil {
		return e
	}
	*s = next
	return nil
}
func PrepareRenewal(dir string, s *State) error {
	if _, e := material(s.Active, s.HostID, time.Now()); e != nil {
		return e
	}
	if s.Pending != nil {
		return validate(s)
	}
	p, e := newPending(s.HostID, s.Active.IdentityID)
	if e != nil {
		return e
	}
	next := *s
	next.Pending = p
	if e = Save(dir, &next); e != nil {
		return e
	}
	*s = next
	return nil
}
func AcceptPending(dir string, s *State, r Response) error {
	if s.Active == nil {
		return ErrIdentity
	}
	m, e := acceptResponse(s, r)
	if e != nil {
		return e
	}
	pending := *s.Pending
	pending.Certificate = m.Certificate
	pending.IssuerCertificate = m.IssuerCertificate
	pending.IdentityID = m.IdentityID
	next := *s
	next.Pending = &pending
	if e = Save(dir, &next); e != nil {
		return e
	}
	*s = next
	return nil
}
func Promote(dir string, s *State) error {
	if s.Active == nil || s.Pending == nil || s.Pending.PredecessorIdentityID != s.Active.IdentityID {
		return ErrIdentity
	}
	if _, e := material(s.Pending.material(), s.HostID, time.Now()); e != nil {
		return e
	}
	next := *s
	next.Active = s.Pending.material()
	next.Pending = nil
	if e := Save(dir, &next); e != nil {
		return e
	}
	*s = next
	return nil
}
func TLSConfig(s *State, pending bool) (*tls.Config, error) {
	roots, e := x509.SystemCertPool()
	if e != nil {
		return nil, ErrIdentity
	}
	if s.ServerCAFile != "" {
		if noSymlinkAncestors(s.ServerCAFile, false) != nil {
			return nil, ErrIdentity
		}
		bytes, e := readCA(s.ServerCAFile)
		if e != nil {
			return nil, ErrIdentity
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(bytes) {
			return nil, ErrIdentity
		}
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, NextProtos: []string{"http/1.1"}}
	m := s.Active
	if pending {
		if s.Pending == nil {
			return nil, ErrIdentity
		}
		m = s.Pending.material()
	}
	if m != nil {
		if _, e = material(m, s.HostID, time.Now()); e != nil {
			return nil, e
		}
		key, e := privateKey(m.PrivateKey)
		if e != nil {
			return nil, e
		}
		leaf, _ := decode64(m.Certificate, 4096)
		issuer, _ := decode64(m.IssuerCertificate, 4096)
		cfg.Certificates = []tls.Certificate{{Certificate: [][]byte{leaf, issuer}, PrivateKey: key}}
	}
	return cfg, nil
}
func Expires(s *State) (time.Time, error) {
	c, e := material(s.Active, s.HostID, time.Now())
	if e != nil {
		return time.Time{}, e
	}
	issuer, _ := certificate(s.Active.IssuerCertificate)
	if issuer.NotAfter.Before(c.NotAfter) {
		return issuer.NotAfter, nil
	}
	return c.NotAfter, nil
}

// Expired reports whether a loaded active credential, or its issuer, is past its
// validity end. Load accepts such state so that recovery can preserve it.
func Expired(s *State) bool {
	if s == nil || s.Active == nil {
		return false
	}
	now := time.Now()
	for _, v := range []string{s.Active.Certificate, s.Active.IssuerCertificate} {
		if c, e := certificate(v); e != nil || !now.Before(c.NotAfter) {
			return true
		}
	}
	return false
}

// PendingExpired reports whether an issued pending credential, or its issuer, is
// past its validity end and so can never be activated.
func PendingExpired(s *State) bool {
	if s == nil || s.Pending == nil || s.Pending.Certificate == "" {
		return false
	}
	now := time.Now()
	for _, v := range []string{s.Pending.Certificate, s.Pending.IssuerCertificate} {
		if c, e := certificate(v); e != nil || !now.Before(c.NotAfter) {
			return true
		}
	}
	return false
}

func noSymlinkAncestors(path string, missingFinal bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrIdentity
	}
	parts := strings.Split(strings.TrimPrefix(path, string(os.PathSeparator)), string(os.PathSeparator))
	current := string(os.PathSeparator)
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) && missingFinal && index == len(parts)-1 {
			return nil
		}
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrIdentity
		}
	}
	return nil
}
func stateShape(value any) bool {
	m, ok := value.(map[string]any)
	if !ok || !exactKeys(m, []string{"schema_version", "endpoint", "server_ca_file", "owner_id", "host_id", "active", "pending"}) {
		return false
	}
	version, ok := m["schema_version"].(*big.Int)
	if !ok || version.Cmp(big.NewInt(1)) != 0 {
		return false
	}
	for _, k := range []string{"endpoint", "server_ca_file", "owner_id", "host_id"} {
		if _, ok := m[k].(string); !ok {
			return false
		}
	}
	for _, slot := range []string{"active", "pending"} {
		if m[slot] == nil {
			continue
		}
		obj, ok := m[slot].(map[string]any)
		if !ok {
			return false
		}
		keys := []string{"identity_id", "private_key", "certificate", "issuer_certificate"}
		if slot == "pending" {
			keys = append(keys, "csr", "predecessor_identity_id")
		}
		if !exactKeys(obj, keys) {
			return false
		}
		for _, k := range keys {
			if _, ok := obj[k].(string); !ok {
				return false
			}
		}
	}
	return true
}
func exactKeys(m map[string]any, keys []string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// readCA opens the CA without following symlinks and checks the open file, so
// no stat-then-open race can swap it. Ancestor-directory ownership is not
// checked (known limitation, out of scope for #86).
func readCA(path string) ([]byte, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, ErrIdentity
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 32768 || info.Mode().Perm()&0022 != 0 {
		return nil, ErrIdentity
	}
	if unix, ok := info.Sys().(*syscall.Stat_t); !ok || (unix.Uid != 0 && unix.Uid != uint32(os.Geteuid())) {
		return nil, ErrIdentity
	}
	data, e := io.ReadAll(io.LimitReader(f, 32769))
	if e != nil || len(data) > 32768 {
		return nil, ErrIdentity
	}
	return data, nil
}
