package connection

import (
	"context"
	"encoding/json"
	"encoding/pem"
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
	dir, state, response := savedPendingDir(t)
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
	go func() { done <- session(context.Background(), state, start.Add(1500*time.Millisecond), nil) }()
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
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("socket was not closed at the deadline")
	}
}
