// SPDX-License-Identifier: EUPL-1.2
package connection

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/identity"
)

var ErrTransport = errors.New("host_transport_unavailable")

func client(state *identity.State, pending bool) (*http.Client, error) {
	cfg, e := identity.TLSConfig(state, pending)
	if e != nil {
		return nil, ErrTransport
	}
	transport := &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}, DisableCompression: true, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 10 * time.Second, MaxResponseHeaderBytes: 8192}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrTransport }}, nil
}
func exact(m map[string]any, keys ...string) bool {
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
func version(m map[string]any) bool {
	v, ok := m["schema_version"].(*big.Int)
	return ok && v.Cmp(big.NewInt(1)) == 0
}
func decodeIdentityResponse(raw []byte) (identity.Response, error) {
	var r identity.Response
	if len(raw) > 16384 {
		return r, ErrTransport
	}
	v, e := wirejson.Parse(raw)
	if e != nil {
		return r, ErrTransport
	}
	m, ok := v.(map[string]any)
	if !ok || !exact(m, "schema_version", "owner_id", "host_id", "identity_id", "certificate", "issuer_certificate", "expires_at") || !version(m) {
		return r, ErrTransport
	}
	for _, k := range []string{"owner_id", "host_id", "identity_id", "certificate", "issuer_certificate"} {
		if _, ok := m[k].(string); !ok {
			return r, ErrTransport
		}
	}
	expiry, ok := m["expires_at"].(*big.Int)
	if !ok || !expiry.IsInt64() || expiry.Sign() <= 0 {
		return r, ErrTransport
	}
	if e = json.Unmarshal(raw, &r); e != nil {
		return r, ErrTransport
	}
	return r, nil
}
func post(ctx context.Context, c *http.Client, endpoint, path string, body any) ([]byte, error) {
	raw, e := json.Marshal(body)
	if e != nil || len(raw) > 16384 {
		return nil, ErrTransport
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(raw))
	if e != nil {
		return nil, ErrTransport
	}
	request.Header.Set("Content-Type", "application/json")
	response, e := c.Do(request)
	if e != nil {
		return nil, ErrTransport
	}
	defer response.Body.Close()
	media, _, e := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if e != nil || media != "application/json" || response.StatusCode != 200 || response.ProtoMajor != 1 {
		return nil, ErrTransport
	}
	raw, e = io.ReadAll(io.LimitReader(response.Body, 16385))
	if e != nil || len(raw) > 16384 {
		return nil, ErrTransport
	}
	return raw, nil
}
func Enroll(ctx context.Context, dir, code string) error {
	if !strings.HasPrefix(code, "whe1_") || len(code) != 48 {
		return ErrTransport
	}
	b, e := base64.RawURLEncoding.DecodeString(code[5:])
	if e != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != code[5:] {
		return ErrTransport
	}
	state, e := identity.Load(dir)
	if e != nil || state.Active != nil || state.Pending == nil {
		return ErrTransport
	}
	c, e := client(state, false)
	if e != nil {
		return e
	}
	defer c.CloseIdleConnections()
	raw, e := post(ctx, c, state.Endpoint, "/agent/v1/enroll", map[string]any{"schema_version": 1, "code": code, "csr": state.Pending.CSR})
	if e != nil {
		return e
	}
	r, e := decodeIdentityResponse(raw)
	if e != nil {
		return e
	}
	if e = identity.AcceptEnrollment(dir, state, r); e != nil {
		return ErrTransport
	}
	return nil
}
func Renew(ctx context.Context, dir string) error {
	state, e := identity.Load(dir)
	if e != nil || state.Active == nil {
		return ErrTransport
	}
	// A saved issued pending credential may already be current on the server.
	// Attempt its activation before checking the historical predecessor's expiry.
	if state.Pending == nil || state.Pending.Certificate == "" {
		if e = identity.PrepareRenewal(dir, state); e != nil {
			return ErrTransport
		}
		c, e := client(state, false)
		if e != nil {
			return e
		}
		defer c.CloseIdleConnections()
		raw, e := post(ctx, c, state.Endpoint, "/agent/v1/identity/renew", map[string]any{"schema_version": 1, "csr": state.Pending.CSR})
		if e != nil {
			return e
		}
		r, e := decodeIdentityResponse(raw)
		if e != nil {
			return e
		}
		if e = identity.AcceptPending(dir, state, r); e != nil {
			return ErrTransport
		}
	}
	c, e := client(state, true)
	if e != nil {
		return e
	}
	defer c.CloseIdleConnections()
	raw, e := post(ctx, c, state.Endpoint, "/agent/v1/identity/activate", map[string]any{"schema_version": 1})
	if e != nil {
		return e
	}
	if !activationMatches(raw, state) {
		return ErrTransport
	}
	if e = identity.Promote(dir, state); e != nil {
		return ErrTransport
	}
	return nil
}

func activationMatches(raw []byte, state *identity.State) bool {
	r, e := decodeIdentityResponse(raw)
	if e != nil || state == nil || state.Pending == nil {
		return false
	}
	p := state.Pending
	if r.OwnerID != state.OwnerID || r.HostID != state.HostID || r.IdentityID != p.IdentityID || r.Certificate != p.Certificate || r.IssuerCertificate != p.IssuerCertificate {
		return false
	}
	der, e := base64.StdEncoding.DecodeString(p.Certificate)
	if e != nil {
		return false
	}
	cert, e := x509.ParseCertificate(der)
	return e == nil && cert.NotAfter.Unix() == r.ExpiresAt
}

// Event carries only fixed status and public host identity, never peer diagnostics.
type Event struct{ Kind, HostID string }

func emit(f func(Event), kind, host string) {
	if f != nil {
		f(Event{kind, host})
	}
}
func send(ctx context.Context, c *websocket.Conn, state *identity.State, kind string) error {
	if _, e := identity.Expires(state); e != nil {
		return ErrTransport
	}
	m := map[string]any{"schema_version": 1, "kind": kind, "owner_id": state.OwnerID, "host_id": state.HostID}
	if kind == "hello" {
		m["capabilities"] = []string{}
	}
	raw, _ := json.Marshal(m)
	if e := c.Write(ctx, websocket.MessageText, raw); e != nil {
		return ErrTransport
	}
	return nil
}

type incomingMessage struct {
	kind string
	err  error
}

func readMessages(ctx context.Context, c *websocket.Conn, state *identity.State, out chan<- incomingMessage) {
	defer close(out)
	for {
		typ, raw, e := c.Read(ctx)
		message := incomingMessage{err: ErrTransport}
		if e == nil && typ == websocket.MessageText {
			if _, validErr := identity.Expires(state); validErr == nil {
				if m, decodeErr := DecodeMessage(raw, state.OwnerID, state.HostID); decodeErr == nil {
					message = incomingMessage{kind: m["kind"].(string)}
				}
			}
		}
		select {
		case out <- message:
		case <-ctx.Done():
			return
		}
		if message.err != nil {
			return
		}
	}
}
func ack(ctx context.Context, in <-chan incomingMessage, kind string) error {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ErrTransport
	case <-timer.C:
		return ErrTransport
	case m, ok := <-in:
		if !ok || m.err != nil || m.kind != kind {
			return ErrTransport
		}
		return nil
	}
}
func session(ctx context.Context, state *identity.State, onEvent func(Event)) error {
	expires, e := identity.Expires(state)
	if e != nil {
		return ErrTransport
	}
	// End an idle socket at actual expiry, or at renewal time, independently of IO.
	deadline := expires.Add(-6 * time.Hour)
	if !deadline.After(time.Now()) {
		deadline = expires
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	c, e := client(state, false)
	if e != nil {
		return e
	}
	defer c.CloseIdleConnections()
	ws, response, e := websocket.Dial(ctx, "wss"+strings.TrimPrefix(state.Endpoint, "https")+"/agent/v1/connect", &websocket.DialOptions{HTTPClient: c, CompressionMode: websocket.CompressionDisabled})
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if e != nil {
		return ErrTransport
	}
	defer ws.CloseNow()
	ws.SetReadLimit(4096)
	incoming := make(chan incomingMessage, 1)
	go readMessages(ctx, ws, state, incoming)
	go watchValidity(ctx, func() bool { _, err := identity.Expires(state); return err == nil }, func() { cancel(); _ = ws.CloseNow() })
	if e = send(ctx, ws, state, "hello"); e != nil {
		return e
	}
	if e = ack(ctx, incoming, "hello_ack"); e != nil {
		return e
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if e = send(ctx, ws, state, "heartbeat"); e != nil {
			return e
		}
		if e = ack(ctx, incoming, "heartbeat_ack"); e != nil {
			return e
		}
		emit(onEvent, "connected", state.HostID)
	wait:
		for {
			select {
			case <-ctx.Done():
				return ErrTransport
			case <-incoming:
				// A close or unsolicited application message ends this transport.
				return ErrTransport
			case <-ticker.C:
				break wait
			}
		}
	}
}

// Validity monitoring runs independently of handshake acknowledgements and reads.
func watchValidity(ctx context.Context, valid func() bool, expire func()) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !valid() {
				expire()
				return
			}
		}
	}
}

// Run reconnects transport only. Lifecycle command admission and replay belong to T010.
func Run(ctx context.Context, dir string, onEvent func(Event)) error {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		state, e := identity.Load(dir)
		if e != nil || state.Active == nil {
			return ErrTransport
		}
		expires, e := identity.Expires(state)
		if (state.Pending != nil && state.Pending.Certificate != "") || (e == nil && time.Until(expires) <= 6*time.Hour) {
			e = Renew(ctx, dir)
			if e == nil {
				state, e = identity.Load(dir)
			}
		}
		if e == nil {
			e = session(ctx, state, func(event Event) { attempt = 0; emit(onEvent, event.Kind, event.HostID) })
		}
		if ctx.Err() != nil {
			return nil
		}
		emit(onEvent, "disconnected", state.HostID)
		delay := time.NewTimer(time.Duration(Backoff(attempt)) * time.Second)
		attempt++
		select {
		case <-ctx.Done():
			if !delay.Stop() {
				<-delay.C
			}
			return nil
		case <-delay.C:
		}
	}
}
