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
	return savedDirAt(t, expired, pending, 0)
}

// savedDirAt issues the pending credential pendingAge in the past (its leaf lasts 24h).
func savedDirAt(t *testing.T, expired, pending bool, pendingAge time.Duration) (string, *identity.State, identity.Response) {
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
	if pendingAge > 0 {
		state.Pending.Certificate = sign(state.Pending.CSR, state.Pending.IdentityID, now.Add(-pendingAge)).Certificate
		if e = identity.Save(dir, state); e != nil {
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
	oldRenew, oldSession, oldNow := renewFn, sessionFn, runNow
	defer func() { renewFn, sessionFn, runNow = oldRenew, oldSession, oldNow }()
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
	now := time.Now()
	runNow = func() time.Time { return now }
	var limit time.Time
	sessionFn = func(_ context.Context, _ *identity.State, l time.Time, _ func(Event)) error {
		if limit.IsZero() {
			limit = l
			now = l
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

func TestExpiredCountsAnExpiredIssuerWithValidLeaf(t *testing.T) {
	// Load cannot produce this pairing (a leaf may not outlive its issuer), so
	// build the in-memory state directly to pin the issuer check.
	_, state, _ := savedDir(t, false, false)
	if identity.Expired(state) {
		t.Fatal("fixture must start valid")
	}
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-2 * time.Hour)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(9), Subject: pkix.Name{CommonName: "old"}, NotBefore: past.Add(-time.Hour), NotAfter: past}
	der, e := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	state.Active.IssuerCertificate = base64.StdEncoding.EncodeToString(der)
	if !identity.Expired(state) {
		t.Fatal("expired issuer with valid leaf not reported expired")
	}
	state.Pending = &identity.Pending{Certificate: state.Active.Certificate, IssuerCertificate: state.Active.IssuerCertificate}
	if !identity.PendingExpired(state) {
		t.Fatal("expired pending issuer with valid leaf not reported expired")
	}
}

// An early socket close must not turn each reconnect into another activation POST.
func TestRunEarlyReconnectHonorsPendingActivationDue(t *testing.T) {
	dir, _, _ := savedPendingDir(t)
	oldRenew, oldSession := renewFn, sessionFn
	defer func() { renewFn, sessionFn = oldRenew, oldSession }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	renews, sessions := 0, 0
	renewFn = func(context.Context, string) error { renews++; return ErrTransport }
	sessionFn = func(_ context.Context, _ *identity.State, limit time.Time, event func(Event)) error {
		sessions++
		event(Event{Kind: "connected", HostID: host})
		if sessions == 2 {
			cancel()
			if renews != 1 {
				t.Errorf("early reconnect attempted activation %d times, want 1 before retry due", renews)
			}
		}
		return ErrTransport
	}
	if err := Run(ctx, dir, nil); err != nil {
		t.Fatal(err)
	}
	if sessions != 2 {
		t.Fatalf("sessions = %d, want 2", sessions)
	}
}

func TestActivationRetryGrowthSaturatesAndResets(t *testing.T) {
	_, state, _ := savedPendingDir(t)
	var retry activationRetry
	retry.sync(state)
	now := time.Now()
	expires := now.Add(48 * time.Hour)
	for i, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute} {
		got := retry.failed(now, expires, true).Sub(now)
		if got != want {
			t.Fatalf("failure %d: delay = %v, want %v", i+1, got, want)
		}
	}
	for i := 0; i < 10000; i++ {
		if got := retry.failed(now, expires, true).Sub(now); got != 30*time.Minute {
			t.Fatalf("saturated delay = %v", got)
		}
	}
	state.Pending.IdentityID = "55555555-5555-4555-8555-555555555555"
	retry.sync(state)
	if !retry.next.IsZero() || retry.delay != 0 {
		t.Fatal("changed pending identity retained backoff")
	}
	if got := retry.failed(now, expires, true).Sub(now); got != 30*time.Second {
		t.Fatalf("new identity delay = %v", got)
	}
	state.Pending = nil
	retry.sync(state)
	if !retry.next.IsZero() || retry.delay != 0 {
		t.Fatal("removed/promoted pending retained backoff")
	}
}

func TestActivationRetryRenewalBoundaryAndExpiry(t *testing.T) {
	_, state, _ := savedPendingDir(t)
	now := time.Now()
	for _, remaining := range []time.Duration{6*time.Hour + 10*time.Second, 6 * time.Hour, time.Hour, time.Second, 0, -time.Hour} {
		var retry activationRetry
		retry.sync(state)
		retry.delay = 30 * time.Minute
		expires := now.Add(remaining)
		got := retry.failed(now, expires, remaining > 0)
		want := now.Add(30 * time.Second)
		if remaining > 6*time.Hour {
			want = expires.Add(-6 * time.Hour)
		}
		if remaining > 0 && remaining < 30*time.Second {
			want = expires
		}
		if !got.Equal(want) {
			t.Errorf("remaining %v: due = %v, want %v", remaining, got, want)
		}
	}
	// Once a clamped retry reaches the renewal boundary, use the fast cadence.
	var retry activationRetry
	retry.sync(state)
	retry.delay = 30 * time.Minute
	expires := now.Add(6*time.Hour + time.Minute)
	boundary := retry.failed(now, expires, true)
	if got := retry.failed(boundary, expires, true).Sub(boundary); got != 30*time.Second {
		t.Fatalf("boundary delay = %v", got)
	}
}

func TestRunHeartbeatsPreservePendingActivationGrowth(t *testing.T) {
	dir, _, _ := savedPendingDir(t)
	oldRenew, oldSession, oldNow := renewFn, sessionFn, runNow
	defer func() { renewFn, sessionFn, runNow = oldRenew, oldSession, oldNow }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now()
	runNow = func() time.Time { return now }
	renews, sessions := 0, 0
	renewFn = func(context.Context, string) error { renews++; return ErrTransport }
	var first time.Time
	sessionFn = func(_ context.Context, _ *identity.State, limit time.Time, event func(Event)) error {
		sessions++
		event(Event{Kind: "connected", HostID: host})
		switch sessions {
		case 1:
			if renews != 1 || limit.Sub(now) != 30*time.Second {
				t.Fatalf("first retry: renews=%d delay=%v", renews, limit.Sub(now))
			}
			first = limit
			now = now.Add(time.Second)
		case 2:
			if renews != 1 || !limit.Equal(first) {
				t.Fatalf("heartbeat/early reconnect bypassed due: renews=%d limit=%v", renews, limit)
			}
			now = limit
		case 3:
			if renews != 2 || limit.Sub(now) != time.Minute {
				t.Fatalf("second retry: renews=%d delay=%v", renews, limit.Sub(now))
			}
			now = limit
		case 4:
			if renews != 3 || limit.Sub(now) != 2*time.Minute {
				t.Fatalf("third retry: renews=%d delay=%v", renews, limit.Sub(now))
			}
			cancel()
		default:
			t.Fatal("unexpected session")
		}
		return ErrTransport
	}
	if err := Run(ctx, dir, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunPromotionClearsPendingActivationDeadline(t *testing.T) {
	dir, _, _ := savedPendingDir(t)
	oldRenew, oldSession, oldNow := renewFn, sessionFn, runNow
	defer func() { renewFn, sessionFn, runNow = oldRenew, oldSession, oldNow }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now()
	runNow = func() time.Time { return now }
	renews := 0
	renewFn = func(context.Context, string) error {
		renews++
		if renews == 1 {
			return ErrTransport
		}
		state, err := identity.Load(dir)
		if err != nil {
			return err
		}
		return identity.Promote(dir, state)
	}
	sessionFn = func(_ context.Context, state *identity.State, limit time.Time, event func(Event)) error {
		event(Event{Kind: "connected", HostID: host})
		if renews == 1 {
			now = limit
		} else {
			if state.Pending != nil || !limit.IsZero() {
				t.Fatal("promotion retained pending retry deadline")
			}
			cancel()
		}
		return ErrTransport
	}
	if err := Run(ctx, dir, nil); err != nil {
		t.Fatal(err)
	}
	if renews != 2 {
		t.Fatalf("renewals = %d", renews)
	}
}

func TestRunCancellationDuringPendingRetry(t *testing.T) {
	dir, _, _ := savedPendingDir(t)
	oldRenew, oldSession := renewFn, sessionFn
	defer func() { renewFn, sessionFn = oldRenew, oldSession }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	renews := 0
	renewFn = func(context.Context, string) error { renews++; return ErrTransport }
	sessionFn = func(context.Context, *identity.State, time.Time, func(Event)) error { return ErrTransport }
	start := time.Now()
	err := Run(ctx, dir, func(event Event) {
		if event.Kind == "disconnected" {
			time.AfterFunc(10*time.Millisecond, cancel)
		}
	})
	if err != nil || renews != 1 || time.Since(start) > time.Second {
		t.Fatalf("cancel: err=%v renews=%d elapsed=%v", err, renews, time.Since(start))
	}
}

func TestRunDoesNotStageOutsideRenewalWindow(t *testing.T) {
	dir, _, _ := savedDir(t, false, false)
	oldRenew, oldSession := renewFn, sessionFn
	defer func() { renewFn, sessionFn = oldRenew, oldSession }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	renewFn = func(context.Context, string) error { t.Fatal("staging outside six-hour window"); return ErrTransport }
	sessionFn = func(_ context.Context, _ *identity.State, limit time.Time, _ func(Event)) error {
		if !limit.IsZero() {
			t.Fatal("unexpected activation deadline without pending identity")
		}
		cancel()
		return ErrTransport
	}
	if err := Run(ctx, dir, nil); err != nil {
		t.Fatal(err)
	}
}

// Keep elapsed scheduling time fixed while advancing only the UTC clock. Advancing
// runNow with Add alone advances both clocks and misses this transition.
func TestRunForwardWallClockJumpRestoresFastPendingRetry(t *testing.T) {
	dir, state, _ := savedPendingDir(t)
	oldRenew, oldSession, oldNow, oldWall := renewFn, sessionFn, runNow, retryWallNow
	defer func() { renewFn, sessionFn, runNow, retryWallNow = oldRenew, oldSession, oldNow, oldWall }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	expires, err := identity.Expires(state)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	now = now.Add(expires.Add(-8 * time.Hour).Sub(now))
	runNow = func() time.Time { return now }
	wallOffset := time.Duration(0)
	retryWallNow = func() time.Time { return now.Round(0).Add(wallOffset) }
	renews, sessions := 0, 0
	renewFn = func(context.Context, string) error { renews++; return ErrTransport }
	sessionFn = func(_ context.Context, _ *identity.State, limit time.Time, event func(Event)) error {
		sessions++
		event(Event{Kind: "connected", HostID: host})
		if sessions < 7 {
			now = limit
			return ErrTransport
		}
		if sessions == 7 {
			// A separate wall-clock observation models a UTC correction without
			// modifying time.Time's private monotonic representation.
			wallOffset = 2 * time.Hour
			return ErrTransport
		}
		cancel()
		if renews != 8 || limit.Sub(now) > 30*time.Second {
			t.Errorf("inside six-hour window retained retry=%v renews=%d; want <=30s and immediate attempt 8", limit.Sub(now), renews)
		}
		return ErrTransport
	}
	if err := Run(ctx, dir, nil); err != nil {
		t.Fatal(err)
	}
}
