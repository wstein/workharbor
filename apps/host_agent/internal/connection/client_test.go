// SPDX-License-Identifier: EUPL-1.2
package connection

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/identity"
	"math/big"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdentityResponseRequiresClosedExactFieldsAndIntegers(t *testing.T) {
	valid := `{"schema_version":1,"owner_id":"` + owner + `","host_id":"` + host + `","identity_id":"33333333-3333-4333-8333-333333333333","certificate":"AA==","issuer_certificate":"AA==","expires_at":123}`
	if _, e := decodeIdentityResponse([]byte(valid)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(valid, `"schema_version":1`, `"SCHEMA_VERSION":1`, 1), strings.Replace(valid, `"expires_at":123`, `"expires_at":123.0`, 1), strings.Replace(valid, `"certificate":"AA==",`, ``, 1), strings.Replace(valid, `"host_id"`, `"host_id":null,"host_id"`, 1), strings.TrimSuffix(valid, "}") + `,"secret":"SECRET_SENTINEL"}`, valid + strings.Repeat(" ", 16384)} {
		if _, e := decodeIdentityResponse([]byte(raw)); e == nil || strings.Contains(e.Error(), "SECRET_SENTINEL") {
			t.Fatal("unsafe response boundary")
		}
	}
}
func TestEnrollmentCodeAndUnsafeStateFailBeforeTransport(t *testing.T) {
	for _, code := range []string{"", "SECRET_SENTINEL", "whe1_" + strings.Repeat("A", 42), "whe1_" + strings.Repeat("!", 43)} {
		if e := Enroll(context.Background(), "/missing/identity", code); e == nil || strings.Contains(e.Error(), "SECRET_SENTINEL") {
			t.Fatal("unsafe enrollment boundary")
		}
	}
	if e := Renew(context.Background(), "/missing/identity"); e == nil {
		t.Fatal("missing possession admitted")
	}
}

func TestActivationRejectsIncompleteOrMismatchedIssuedIdentity(t *testing.T) {
	state, response := savedPending(t)
	raw, _ := json.Marshal(response)
	if !activationMatches(raw, state) {
		t.Fatal("valid saved pending response refused")
	}
	var fields map[string]any
	if e := json.Unmarshal(raw, &fields); e != nil {
		t.Fatal(e)
	}
	for key := range fields {
		t.Run("missing_"+key, func(t *testing.T) {
			bad := map[string]any{}
			for k, v := range fields {
				if k != key {
					bad[k] = v
				}
			}
			bytes, _ := json.Marshal(bad)
			if activationMatches(bytes, state) {
				t.Fatal("missing field admitted")
			}
		})
		t.Run("changed_"+key, func(t *testing.T) {
			bad := map[string]any{}
			for k, v := range fields {
				bad[k] = v
			}
			switch key {
			case "schema_version":
				bad[key] = 2
			case "expires_at":
				bad[key] = fields[key].(float64) + 1
			default:
				bad[key] = "SECRET_SENTINEL"
			}
			bytes, _ := json.Marshal(bad)
			if activationMatches(bytes, state) {
				t.Fatal("changed field admitted")
			}
		})
	}
	if activationMatches([]byte(`{"schema_version":1,"owner_id":"`+owner+`","host_id":"`+host+`","identity_id":"33333333-3333-4333-8333-333333333333"}`), state) {
		t.Fatal("short ack admitted")
	}
}

func savedPending(t *testing.T) (*identity.State, identity.Response) {
	t.Helper()
	temp, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(temp, "identity")
	state, e := identity.Prepare(dir, "https://localhost:8443", "")
	if e != nil {
		t.Fatal(e)
	}
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().Truncate(time.Second).Add(-time.Second)
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "WorkHarbor client issuer"}, NotBefore: now, NotAfter: now.Add(5 * 365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	ca, e := x509.CreateCertificate(rand.Reader, root, root, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	issuer, e := x509.ParseCertificate(ca)
	if e != nil {
		t.Fatal(e)
	}
	sign := func(csr64, id string) identity.Response {
		csrDER, e := base64.StdEncoding.DecodeString(csr64)
		if e != nil {
			t.Fatal(e)
		}
		csr, e := x509.ParseCertificateRequest(csrDER)
		if e != nil {
			t.Fatal(e)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: host}, NotBefore: now, NotAfter: now.Add(24 * time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		der, e := x509.CreateCertificate(rand.Reader, leaf, issuer, csr.PublicKey, key)
		if e != nil {
			t.Fatal(e)
		}
		return identity.Response{SchemaVersion: 1, OwnerID: owner, HostID: host, IdentityID: id, Certificate: base64.StdEncoding.EncodeToString(der), IssuerCertificate: base64.StdEncoding.EncodeToString(ca), ExpiresAt: leaf.NotAfter.Unix()}
	}
	if e = identity.AcceptEnrollment(dir, state, sign(state.Pending.CSR, "33333333-3333-4333-8333-333333333333")); e != nil {
		t.Fatal(e)
	}
	if e = identity.PrepareRenewal(dir, state); e != nil {
		t.Fatal(e)
	}
	response := sign(state.Pending.CSR, "44444444-4444-4444-8444-444444444444")
	if e = identity.AcceptPending(dir, state, response); e != nil {
		t.Fatal(e)
	}
	state, e = identity.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	return state, response
}

func TestExpiryWatchdogCancelsBlockedIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var valid atomic.Bool
	valid.Store(true)
	expired := make(chan struct{})
	go watchValidity(ctx, func() bool { return valid.Load() }, func() { cancel(); close(expired) })
	// No heartbeat/wait loop participates: the simulated IO remains blocked.
	valid.Store(false)
	select {
	case <-expired:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("expiry watchdog waited for IO")
	}
	if ctx.Err() == nil {
		t.Fatal("blocked IO context not cancelled")
	}
}

func TestAcknowledgementSequenceFailsClosed(t *testing.T) {
	for _, name := range []string{"valid", "wrongkind", "closed", "error", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			in := make(chan incomingMessage, 1)
			switch name {
			case "valid":
				in <- incomingMessage{kind: "hello_ack"}
			case "wrongkind":
				in <- incomingMessage{kind: "heartbeat_ack"}
			case "closed":
				close(in)
			case "error":
				in <- incomingMessage{err: ErrTransport}
			case "cancelled":
				cancel()
			}
			e := ack(ctx, in, "hello_ack")
			if (e == nil) != (name == "valid") {
				t.Fatal("ack sequence boundary changed")
			}
		})
	}
}
