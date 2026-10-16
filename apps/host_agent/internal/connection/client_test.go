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
	_, state, response := savedPendingDir(t)
	return state, response
}

func savedPendingDir(t *testing.T) (string, *identity.State, identity.Response) {
	t.Helper()
	return savedDir(t, false, true)
}

// savedExpiredDir returns an identity whose active credential ended an hour ago,
// with or without an issued pending credential.
func savedExpiredDir(t *testing.T, pending bool) (string, *identity.State) {
	t.Helper()
	dir, state, _ := savedDir(t, true, pending)
	return dir, state
}

func savedDir(t *testing.T, expired, pending bool) (string, *identity.State, identity.Response) {
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
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "WorkHarbor client issuer"}, NotBefore: now.Add(-48 * time.Hour), NotAfter: now.Add(-48*time.Hour + 5*365*24*time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	ca, e := x509.CreateCertificate(rand.Reader, root, root, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	issuer, e := x509.ParseCertificate(ca)
	if e != nil {
		t.Fatal(e)
	}
	sign := func(csr64, id string, now time.Time) identity.Response {
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
	activeCSR := state.Pending.CSR
	if e = identity.AcceptEnrollment(dir, state, sign(activeCSR, "33333333-3333-4333-8333-333333333333", now)); e != nil {
		t.Fatal(e)
	}
	var response identity.Response
	if pending {
		if e = identity.PrepareRenewal(dir, state); e != nil {
			t.Fatal(e)
		}
		response = sign(state.Pending.CSR, "44444444-4444-4444-8444-444444444444", now)
		if e = identity.AcceptPending(dir, state, response); e != nil {
			t.Fatal(e)
		}
	}
	if expired {
		// Same key, certificate valid from 25h to 1h ago: loadable but past its end.
		state.Active.Certificate = sign(activeCSR, "33333333-3333-4333-8333-333333333333", now.Add(-24*time.Hour)).Certificate
		if e = identity.Save(dir, state); e != nil {
			t.Fatal(e)
		}
	}
	state, e = identity.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	return dir, state, response
}

func TestWatchValidityCancelsOnInvalidIdentity(t *testing.T) {
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

func TestRunRetriesFailedRenewalWhileConnected(t *testing.T) {
	dir, state, _ := savedPendingDir(t)
	expires, e := identity.Expires(state)
	if e != nil {
		t.Fatal(e)
	}
	oldRenew, oldSession := renewFn, sessionFn
	defer func() { renewFn, sessionFn = oldRenew, oldSession }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var renews atomic.Int32
	renewFn = func(context.Context, string) error {
		if renews.Add(1) == 1 {
			return ErrTransport
		}
		cancel()
		return nil
	}
	var limit time.Time
	sessionFn = func(_ context.Context, _ *identity.State, l time.Time, _ func(Event)) error {
		if limit.IsZero() {
			limit = l
		}
		return ErrTransport
	}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, dir, nil) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("renewal was not retried")
	}
	if renews.Load() != 2 {
		t.Fatalf("renewals = %d, want retry after failure", renews.Load())
	}
	if limit.IsZero() || !limit.Before(expires) || time.Until(limit) > renewRetry {
		t.Fatalf("session after failed renewal not bounded to the retry time: %v", limit)
	}
}

func TestRunEndsWithRecoveryErrorForExpiredIdentityWithoutPending(t *testing.T) {
	dir, state := savedExpiredDir(t, false)
	if !identity.Expired(state) || state.Pending != nil {
		t.Fatal("fixture must be expired without pending")
	}
	oldRenew, oldSession := renewFn, sessionFn
	defer func() { renewFn, sessionFn = oldRenew, oldSession }()
	var calls atomic.Int32
	renewFn = func(context.Context, string) error { calls.Add(1); return ErrTransport }
	sessionFn = func(context.Context, *identity.State, time.Time, func(Event)) error {
		calls.Add(1)
		return ErrTransport
	}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), dir, nil) }()
	select {
	case e := <-done:
		if e != ErrRecoveryRequired {
			t.Fatalf("Run = %v, want ErrRecoveryRequired", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept retrying an unrecoverable identity")
	}
	if calls.Load() != 0 {
		t.Fatalf("renewal or session attempted: %d", calls.Load())
	}
	if after, e := identity.Load(dir); e != nil || after.Active.IdentityID != state.Active.IdentityID {
		t.Fatalf("state not preserved: %v", e)
	}
}

func TestRunAttemptsActivationOfPendingAfterActiveExpiry(t *testing.T) {
	dir, state := savedExpiredDir(t, true)
	if !identity.Expired(state) || state.Pending == nil || state.Pending.Certificate == "" {
		t.Fatal("fixture must be expired with an issued pending credential")
	}
	oldRenew, oldSession := renewFn, sessionFn
	defer func() { renewFn, sessionFn = oldRenew, oldSession }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var renews atomic.Int32
	renewFn = func(context.Context, string) error { renews.Add(1); cancel(); return ErrTransport }
	sessionFn = func(context.Context, *identity.State, time.Time, func(Event)) error { return ErrTransport }
	done := make(chan error, 1)
	go func() { done <- Run(ctx, dir, nil) }()
	select {
	case e := <-done:
		if e == ErrRecoveryRequired {
			t.Fatal("pending credential was abandoned for recovery")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if renews.Load() != 1 {
		t.Fatalf("activation attempts = %d, want 1", renews.Load())
	}
}
