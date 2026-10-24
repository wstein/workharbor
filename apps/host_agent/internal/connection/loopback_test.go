package connection

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/identity"
)

func TestSessionDeadlineClampsToRetryLimit(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	if got := sessionDeadline(now, expires, time.Time{}); !got.Equal(expires.Add(-6 * time.Hour)) {
		t.Fatalf("no limit: deadline = %v, want renewal time", got)
	}
	if got := sessionDeadline(now, now.Add(time.Hour), time.Time{}); !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("renewal due: deadline = %v, want expiry", got)
	}
	limit := now.Add(30 * time.Second)
	if got := sessionDeadline(now, expires, limit); !got.Equal(limit) {
		t.Fatalf("limit before renewal time: deadline = %v, want limit", got)
	}
	if got := sessionDeadline(now, now.Add(10*time.Second), limit); !got.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("limit after expiry must not extend the session: %v", got)
	}
}

// loopbackAgent serves the agent API over TLS on loopback. A nil activate fails activation.
func loopbackAgent(t *testing.T, activate http.HandlerFunc, ws func(*websocket.Conn)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var activations atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/v1/identity/activate", func(w http.ResponseWriter, r *http.Request) {
		activations.Add(1)
		if activate == nil {
			http.Error(w, "no", http.StatusInternalServerError)
			return
		}
		activate(w, r)
	})
	mux.HandleFunc("/agent/v1/connect", func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if e != nil {
			return
		}
		defer c.CloseNow()
		ws(c)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv, &activations
}

// handshake acknowledges hello and the first heartbeat, then stays silent until the peer closes.
func handshake(closed chan<- struct{}) func(*websocket.Conn) {
	var once sync.Once
	return func(c *websocket.Conn) {
		ctx := context.Background()
		for _, ack := range []string{"hello_ack", "heartbeat_ack"} {
			_, raw, e := c.Read(ctx)
			if e != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(raw, &m) != nil {
				return
			}
			reply, _ := json.Marshal(map[string]any{"schema_version": 1, "kind": ack, "owner_id": m["owner_id"], "host_id": m["host_id"]})
			if c.Write(ctx, websocket.MessageText, reply) != nil {
				return
			}
		}
		_, _, _ = c.Read(ctx)
		if closed != nil {
			once.Do(func() { close(closed) })
		}
	}
}

// loopbackDir returns an identity dir with an active credential and a saved issued pending one,
// pointed at the loopback server.
func loopbackDir(t *testing.T, srv *httptest.Server) (string, *identity.State, identity.Response) {
	t.Helper()
	return loopbackDirAt(t, srv, false, 0)
}

func loopbackDirAt(t *testing.T, srv *httptest.Server, expired bool, pendingAge time.Duration) (string, *identity.State, identity.Response) {
	t.Helper()
	dir, state, response := savedDirAt(t, expired, true, pendingAge)
	ca := filepath.Join(filepath.Dir(dir), "server-ca.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if e := os.WriteFile(ca, block, 0600); e != nil {
		t.Fatal(e)
	}
	state.Endpoint, state.ServerCAFile = srv.URL, ca
	if e := identity.Save(dir, state); e != nil {
		t.Fatal(e)
	}
	return dir, state, response
}

func activateWith(response *identity.Response) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}

// runUntilConnected drives Run until its first connected event, then stops it.
func runUntilConnected(t *testing.T, dir string) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connected := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, dir, func(e Event) {
			if e.Kind == "connected" {
				select {
				case connected <- struct{}{}:
				default:
				}
			}
		})
	}()
	select {
	case <-connected:
		cancel()
		return <-done
	case e := <-done:
		t.Fatalf("Run returned before connecting: %v", e)
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not connect")
	}
	return nil
}

func TestRunRenewsOverLoopbackThenConnects(t *testing.T) {
	var response identity.Response
	srv, activations := loopbackAgent(t, activateWith(&response), handshake(nil))
	dir, _, r := loopbackDir(t, srv)
	response = r
	if e := runUntilConnected(t, dir); e != nil {
		t.Fatal(e)
	}
	if activations.Load() != 1 {
		t.Fatalf("activations = %d", activations.Load())
	}
	state, e := identity.Load(dir)
	if e != nil || state.Active.IdentityID != r.IdentityID {
		t.Fatalf("renewed identity not promoted: %v", e)
	}
}

func TestRunStaysConnectedWhenRenewalFailsOnValidIdentity(t *testing.T) {
	srv, activations := loopbackAgent(t, nil, handshake(nil))
	dir, before, r := loopbackDir(t, srv)
	if e := runUntilConnected(t, dir); e != nil {
		t.Fatal(e)
	}
	if activations.Load() == 0 {
		t.Fatal("renewal was not attempted")
	}
	state, e := identity.Load(dir)
	if e != nil || state.Active.IdentityID != before.Active.IdentityID || state.Active.IdentityID == r.IdentityID {
		t.Fatalf("failed renewal changed the active identity: %v", e)
	}
}

func TestRunReturnsTransportWhenReloadFailsAfterRenewal(t *testing.T) {
	oldRenew := renewFn
	defer func() { renewFn = oldRenew }()
	dir := ""
	renewFn = func(context.Context, string) error {
		return os.WriteFile(filepath.Join(dir, "identity.json"), []byte("{"), 0600)
	}
	// The server is never contacted: renewFn is stubbed. It stays only because
	// loopbackDir needs a server certificate to build a valid identity fixture.
	srv, _ := loopbackAgent(t, nil, handshake(nil))
	dir, _, _ = loopbackDir(t, srv)
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), dir, nil) }()
	select {
	case e := <-done:
		if e != ErrTransport {
			t.Fatalf("Run = %v, want ErrTransport", e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after failed reload")
	}
}

func TestSessionEndsBlockedReadAtValidityDeadline(t *testing.T) {
	closed := make(chan struct{})
	srv, _ := loopbackAgent(t, nil, handshake(closed))
	dir, _, _ := loopbackDir(t, srv)
	state, e := identity.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	// The credential is valid for hours; only the limit can end the idle socket.
	start := time.Now()
	done := make(chan error, 1)
	var connected atomic.Int32
	go func() {
		done <- session(context.Background(), state, start.Add(1500*time.Millisecond), func(e Event) {
			if e.Kind == "connected" {
				connected.Add(1)
			}
		})
	}()
	select {
	case e = <-done:
		if e != ErrTransport {
			t.Fatalf("session = %v, want ErrTransport", e)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("session did not end at the deadline")
	}
	if time.Since(start) < time.Second {
		t.Fatalf("session ended early: %v", time.Since(start))
	}
	if connected.Load() == 0 {
		t.Fatal("connected was not emitted before the deadline ended the read")
	}
	// This proves the socket is closed, not that session called CloseNow: coder/websocket
	// also closes the connection when its context is cancelled.
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("socket was not closed at the deadline")
	}
}

func refuse(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":"` + code + `"}}`))
	}
}

func runResult(t *testing.T, dir string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), dir, nil) }()
	select {
	case e := <-done:
		return e
	case <-time.After(10 * time.Second):
		t.Fatal("Run kept retrying")
	}
	return nil
}

func TestRunEndsWithRecoveryErrorWhenActivationRefusedAfterExpiry(t *testing.T) {
	for _, refusal := range []struct {
		status int
		code   string
	}{{401, "unauthorized_host"}, {400, "identity_predecessor_expired"}, {400, "identity_predecessor_missing"}} {
		status := refusal.status
		srv, activations := loopbackAgent(t, refuse(status, refusal.code), handshake(nil))
		dir, before, _ := loopbackDirAt(t, srv, true, 0)
		if e := runResult(t, dir); e != ErrRecoveryRequired {
			t.Fatalf("status %d: Run = %v, want ErrRecoveryRequired", status, e)
		}
		if activations.Load() != 1 {
			t.Fatalf("status %d: activations = %d, want 1", status, activations.Load())
		}
		if after, e := identity.Load(dir); e != nil || after.Active.IdentityID != before.Active.IdentityID || after.Pending == nil {
			t.Fatalf("state not preserved: %v", e)
		}
	}
}

func TestRunEndsWithRecoveryErrorForExpiredPendingAfterActiveExpiry(t *testing.T) {
	srv, activations := loopbackAgent(t, nil, handshake(nil))
	dir, _, _ := loopbackDirAt(t, srv, true, 30*time.Hour)
	state, e := identity.Load(dir)
	if e != nil || !identity.Expired(state) || !identity.PendingExpired(state) {
		t.Fatalf("fixture must have expired active and pending: %v", e)
	}
	if e := runResult(t, dir); e != ErrRecoveryRequired {
		t.Fatalf("Run = %v, want ErrRecoveryRequired", e)
	}
	if activations.Load() != 0 {
		t.Fatalf("activation attempted for an expired pending credential: %d", activations.Load())
	}
}

func TestRunKeepsRetryingTransientActivationFailureAfterExpiry(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"500":                   nil,
		"503":                   refuse(503, "host_service_unavailable"),
		"503 read":              refuse(503, "request_read_failed"),
		"403 scheme layer":      refuse(403, "forbidden"),
		"400 unknown code":      refuse(400, "something_new"),
		"400 invalid_request":   refuse(400, "invalid_request"),
		"400 activation_failed": refuse(400, "identity_activation_failed"),
		"401 unknown code":      refuse(401, "something_new"),
		"401 with predecessor":  refuse(401, "identity_predecessor_expired"),
		"400 with unauthorized": refuse(400, "unauthorized_host"),
		"400 empty non-json":    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400) },
	} {
		srv, activations := loopbackAgent(t, handler, handshake(nil))
		dir, _, _ := loopbackDirAt(t, srv, true, 0)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, dir, nil) }()
		deadline := time.After(15 * time.Second)
		for activations.Load() < 2 {
			select {
			case e := <-done:
				t.Fatalf("%s: Run ended (%v) on a transient failure", name, e)
			case <-deadline:
				t.Fatalf("%s: activation not retried", name)
			case <-time.After(50 * time.Millisecond):
			}
		}
		cancel()
		if e := <-done; e != nil {
			t.Fatalf("%s: Run = %v after cancel", name, e)
		}
	}
}

func TestRunKeepsValidIdentityConnectedWhenActivationRefusedBeforeExpiry(t *testing.T) {
	srv, activations := loopbackAgent(t, refuse(401, "unauthorized_host"), handshake(nil))
	dir, before, _ := loopbackDir(t, srv)
	if e := runUntilConnected(t, dir); e != nil {
		t.Fatal(e)
	}
	if activations.Load() == 0 {
		t.Fatal("activation was not attempted")
	}
	if after, e := identity.Load(dir); e != nil || after.Active.IdentityID != before.Active.IdentityID {
		t.Fatalf("refusal changed the active identity: %v", e)
	}
}

func TestTransportOnlyKeepsStagingAndEnrollmentRefusalsRetryable(t *testing.T) {
	for _, in := range []error{errRefused, errForbidden, fmt.Errorf("wrapped: %w", errRefused)} {
		if got := transportOnly(in); got != ErrTransport {
			t.Fatalf("transportOnly(%v) = %v, want ErrTransport", in, got)
		}
	}
	if got := transportOnly(ErrTransport); got != ErrTransport {
		t.Fatalf("transportOnly(ErrTransport) = %v", got)
	}
	// End to end: a final-looking activation refusal on the staging endpoint is ErrTransport.
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/v1/identity/renew", refuse(401, "unauthorized_host"))
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	dir, _, _ := loopbackDirAt(t, srv, false, 0)
	state, e := identity.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	state.Pending.Certificate, state.Pending.IssuerCertificate, state.Pending.IdentityID = "", "", ""
	if e = identity.Save(dir, state); e != nil {
		t.Fatal(e)
	}
	if e = Renew(context.Background(), dir); e != ErrTransport {
		t.Fatalf("Renew staging refusal = %v, want ErrTransport", e)
	}
}

func TestRunReportsForbiddenActivationAndRetries(t *testing.T) {
	srv, activations := loopbackAgent(t, refuse(403, "forbidden"), handshake(nil))
	dir, _, _ := loopbackDirAt(t, srv, true, 0)
	ctx, cancel := context.WithCancel(context.Background())
	var forbidden atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, dir, func(ev Event) {
			if ev.Kind == "activation_forbidden" {
				forbidden.Add(1)
			}
		})
	}()
	deadline := time.After(15 * time.Second)
	for activations.Load() < 2 {
		select {
		case e := <-done:
			t.Fatalf("Run ended (%v) on a 403", e)
		case <-deadline:
			t.Fatal("403 not retried")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-done
	if forbidden.Load() == 0 {
		t.Fatal("403 was not reported")
	}
}

func TestSessionWakesWhenUTCJumpPassesPendingRetryDeadline(t *testing.T) {
	oldWall := retryWallNow
	defer func() { retryWallNow = oldWall }()
	var wallOffset atomic.Int64
	retryWallNow = func() time.Time { return time.Now().Add(time.Duration(wallOffset.Load())).Round(0) }
	srv, _ := loopbackAgent(t, nil, handshake(nil))
	dir, state, _ := loopbackDir(t, srv)
	expires, err := identity.Expires(state)
	if err != nil {
		t.Fatal(err)
	}
	// The active credential remains valid in real time. Only the scheduling wall
	// observation moves into its six-hour window after authenticated heartbeat.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	connected := false
	start := time.Now()
	err = session(ctx, state, start.Add(30*time.Minute), func(event Event) {
		if event.Kind == "connected" {
			connected = true
			wallOffset.Store(int64(expires.Add(-5 * time.Hour).Sub(time.Now())))
		}
	})
	if !connected || ctx.Err() != nil || time.Since(start) >= 3*time.Second {
		t.Fatalf("UTC deadline did not wake connected session: connected=%v context=%v elapsed=%v", connected, ctx.Err(), time.Since(start))
	}
	if err != ErrTransport {
		t.Fatalf("session = %v, want transport close", err)
	}
	if _, err := identity.Expires(state); err != nil {
		t.Fatalf("fixture credential unexpectedly expired: %v", err)
	}
	if _, err := identity.Load(dir); err != nil {
		t.Fatal(err)
	}
}
