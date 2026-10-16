// SPDX-License-Identifier: EUPL-1.2
package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The fault wrappers retain real private files and syscall ordering. Only the
// selected syscall fails; no production environment or network hook exists.
type faultFile struct {
	durableFile
	stage, kind string
}

func (f faultFile) Write(b []byte) (int, error) {
	if f.stage == "write" {
		n, _ := f.durableFile.Write(b[:len(b)/2])
		return n, os.ErrPermission
	}
	return f.durableFile.Write(b)
}
func (f faultFile) Sync() error {
	if f.stage == f.kind+"_sync" {
		return os.ErrPermission
	}
	return f.durableFile.Sync()
}
func (f faultFile) Close() error {
	e := f.durableFile.Close()
	if f.stage == f.kind+"_close" {
		return os.ErrPermission
	}
	return e
}
func failFiles(t *testing.T, stage string) func() {
	t.Helper()
	before := identityFiles
	ops := before
	ops.createTemp = func(d, p string) (durableFile, error) {
		if stage == "create" {
			return nil, os.ErrPermission
		}
		f, e := before.createTemp(d, p)
		return faultFile{f, stage, "file"}, e
	}
	ops.openDir = func(d string) (durableFile, error) {
		if stage == "parent_open" {
			return nil, os.ErrPermission
		}
		f, e := before.openDir(d)
		return faultFile{f, stage, "parent"}, e
	}
	ops.rename = func(a, b string) error {
		if stage == "rename" {
			return os.ErrPermission
		}
		return before.rename(a, b)
	}
	ops.link = func(a, b string) error {
		if stage == "link" {
			return os.ErrPermission
		}
		return before.link(a, b)
	}
	identityFiles = ops
	reset := func() { identityFiles = before }
	t.Cleanup(reset)
	return reset
}
func savedFixture(t *testing.T) (string, *State, func(*State) Response) {
	t.Helper()
	dir := filepath.Join(physicalTemp(t), "identity")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Add(-time.Minute).Truncate(time.Second)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "WorkHarbor client issuer"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(-time.Hour).Add(5 * 365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	issuer, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	s := &State{SchemaVersion: 1, Endpoint: "https://localhost:8443", OwnerID: "11111111-1111-4111-8111-111111111111", HostID: "22222222-2222-4222-8222-222222222222"}
	owner, host := s.OwnerID, s.HostID
	response := func(v *State) Response {
		csrDER, e := base64.StdEncoding.DecodeString(v.Pending.CSR)
		if e != nil {
			t.Fatal(e)
		}
		csr, e := x509.ParseCertificateRequest(csrDER)
		if e != nil {
			t.Fatal(e)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: host}, NotBefore: now, NotAfter: now.Add(24 * time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		cert, e := x509.CreateCertificate(rand.Reader, leaf, issuer, csr.PublicKey, key)
		if e != nil {
			t.Fatal(e)
		}
		return Response{SchemaVersion: 1, OwnerID: owner, HostID: host, IdentityID: "44444444-4444-4444-8444-444444444444", Certificate: base64.StdEncoding.EncodeToString(cert), IssuerCertificate: base64.StdEncoding.EncodeToString(der), ExpiresAt: leaf.NotAfter.Unix()}
	}
	pending, e := newPending(s.HostID, "")
	if e != nil {
		t.Fatal(e)
	}
	s.Pending = pending
	m, e := acceptResponse(s, response(s))
	if e != nil {
		t.Fatal(e)
	}
	m.IdentityID = "33333333-3333-4333-8333-333333333333"
	s.Active = m
	s.Pending = nil
	if e := save(dir, s, true); e != nil {
		t.Fatal(e)
	}
	return dir, s, response
}
func TestAtomicIdentityPersistenceFailuresAndRestartTransitions(t *testing.T) {
	// A separate test process has no caller memory or installed fault wrappers.
	// Only the private path is passed, never credential bytes or private keys.
	if dir := os.Getenv("WORKHARBOR_TEST_DURABLE_RELOAD"); dir != "" {
		if _, e := Load(dir); e != nil {
			t.Fatal("independent durable reload refused")
		}
		return
	}

	for _, transition := range []string{"accept_enrollment", "prepare_renewal", "accept_pending", "promote"} {
		for _, stage := range []string{"create", "write", "file_sync", "file_close", "rename", "parent_open", "parent_sync", "parent_close"} {
			t.Run(transition+"/"+stage, func(t *testing.T) {
				dir, s, response := savedFixture(t)
				var e error
				var r Response
				if transition == "accept_enrollment" {
					dir = filepath.Join(filepath.Dir(dir), "enrollment")
					s, e = Prepare(dir, "https://localhost:8443", "")
					if e != nil {
						t.Fatal(e)
					}
					r = response(s)
				}
				if transition != "prepare_renewal" && transition != "accept_enrollment" {
					if e := PrepareRenewal(dir, s); e != nil {
						t.Fatal(e)
					}
					r = response(s)
				}
				if transition == "promote" {
					if e := AcceptPending(dir, s, r); e != nil {
						t.Fatal(e)
					}
				}
				beforeBytes, e := os.ReadFile(filepath.Join(dir, "identity.json"))
				if e != nil {
					t.Fatal(e)
				}
				var before State
				if e = json.Unmarshal(beforeBytes, &before); e != nil {
					t.Fatal(e)
				}
				apply := func(v *State) error {
					switch transition {
					case "accept_enrollment":
						return AcceptEnrollment(dir, v, r)
					case "prepare_renewal":
						return PrepareRenewal(dir, v)
					case "accept_pending":
						return AcceptPending(dir, v, r)
					default:
						return Promote(dir, v)
					}
				}
				reset := failFiles(t, stage)
				e = apply(s)
				reset()
				if !errors.Is(e, ErrIdentity) {
					t.Fatalf("%s did not return fixed persistence refusal", stage)
				}
				if !reflect.DeepEqual(s, &before) {
					t.Fatal("failed commit reported in-memory success")
				}
				executable, e := os.Executable()
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				process := exec.CommandContext(ctx, executable, "-test.run=^TestAtomicIdentityPersistenceFailuresAndRestartTransitions$")
				process.Env = []string{"WORKHARBOR_TEST_DURABLE_RELOAD=" + dir}
				output, childErr := process.CombinedOutput()
				cancel()
				if childErr != nil || !strings.Contains(string(output), "PASS") {
					t.Fatal("independent restart did not load complete valid state")
				}
				loaded, e := Load(dir)
				if e != nil {
					t.Fatal("restart lost complete valid identity")
				}
				afterBytes, e := os.ReadFile(filepath.Join(dir, "identity.json"))
				if e != nil {
					t.Fatal(e)
				}
				committed := stage == "parent_open" || stage == "parent_sync" || stage == "parent_close"
				if !committed {
					if string(beforeBytes) != string(afterBytes) {
						t.Fatal("pre-commit failure replaced durable identity")
					}
				} else {
					switch transition {
					case "accept_enrollment":
						if loaded.Pending != nil || loaded.OwnerID != r.OwnerID || loaded.HostID != r.HostID || loaded.Active == nil || loaded.Active.IdentityID != r.IdentityID || loaded.Active.PrivateKey != before.Pending.PrivateKey {
							t.Fatal("partial enrollment assignment/key promotion")
						}
					case "prepare_renewal":
						if loaded.Pending == nil || loaded.Pending.PredecessorIdentityID != before.Active.IdentityID {
							t.Fatal("lost complete pending possession")
						}
					case "accept_pending":
						if loaded.Pending == nil || loaded.Pending.Certificate != r.Certificate || loaded.Pending.PrivateKey != before.Pending.PrivateKey {
							t.Fatal("lost issued pending key")
						}
					case "promote":
						if loaded.Pending != nil || loaded.Active.IdentityID != r.IdentityID || loaded.Active.PrivateKey != before.Pending.PrivateKey {
							t.Fatal("partial promotion")
						}
					}
				}
				// Re-enter from disk, never the failed caller's old in-memory state. A fully
				// promoted state is already recoverable; all other stages safely retry.
				if !(committed && (transition == "promote" || transition == "accept_enrollment")) {
					oldKey := ""
					if loaded.Pending != nil {
						oldKey = loaded.Pending.PrivateKey
					}
					if e = apply(loaded); e != nil {
						t.Fatal("restart cannot retry transition")
					}
					again, e := Load(dir)
					if e != nil {
						t.Fatal(e)
					}
					if transition == "accept_enrollment" && (again.Active == nil || again.Active.PrivateKey != oldKey) {
						t.Fatal("enrollment retry replaced pending possession")
					}
					if oldKey != "" && transition != "promote" && transition != "accept_enrollment" && (again.Pending == nil || again.Pending.PrivateKey != oldKey) {
						t.Fatal("retry replaced saved possession")
					}
				}
				files, e := filepath.Glob(filepath.Join(dir, ".identity-*"))
				if e != nil || len(files) != 0 {
					t.Fatal("temporary private material retained")
				}
			})
		}
	}
}
func TestInitialPossessionDurabilityFailureNeverReportsSuccess(t *testing.T) {
	for _, stage := range []string{"create", "write", "file_sync", "file_close", "link", "parent_open", "parent_sync", "parent_close"} {
		t.Run(stage, func(t *testing.T) {
			dir := filepath.Join(physicalTemp(t), "identity")
			real := identityFiles
			reset := failFiles(t, stage)
			if faulty := identityFiles.openDir; strings.HasPrefix(stage, "parent_") {
				// Aim the fault at the identity directory's own sync; the parent
				// sync after Mkdir has its own tests in hardening_test.go.
				identityFiles.openDir = func(p string) (durableFile, error) {
					if p != dir {
						return real.openDir(p)
					}
					return faulty(p)
				}
			}
			s, e := Prepare(dir, "https://localhost:8443", "")
			reset()
			if s != nil || !errors.Is(e, ErrIdentity) {
				t.Fatal("failed initial persist claimed success")
			}
			post := stage == "parent_open" || stage == "parent_sync" || stage == "parent_close"
			loaded, e := Load(dir)
			if post {
				if e != nil || loaded.Active != nil || loaded.Pending == nil {
					t.Fatal("restart lost whole initial pending identity")
				}
				bytes, _ := os.ReadFile(filepath.Join(dir, "identity.json"))
				if _, e = Prepare(dir, "https://localhost:8443", ""); e == nil {
					t.Fatal("replaced uncertain committed identity")
				}
				after, _ := os.ReadFile(filepath.Join(dir, "identity.json"))
				if string(bytes) != string(after) {
					t.Fatal("exclusive retry overwrote identity")
				}
			} else {
				if e == nil {
					t.Fatal("pre-commit failure created identity")
				}
				if _, e = Prepare(dir, "https://localhost:8443", ""); e != nil {
					t.Fatal("clean retry failed")
				}
			}
		})
	}
}
