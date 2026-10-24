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

// ErrRecoveryRequired ends Run when the active credential has expired and no issued
// pending credential remains to activate. All state is preserved; only an owner
// newcode enrollment can restore the host.
var ErrRecoveryRequired = errors.New("host_identity_expired_owner_newcode_recovery_required")

// errRefused marks a final activation refusal: only the body codes the control plane
// sends for a real refusal (see finalRefusal). Everything else, including unknown 4xx
// codes and every 5xx (the control plane answers 503 host_service_unavailable and
// request_read_failed for transient faults), is ErrTransport and retries.
var errRefused = errors.New("host_request_refused")

// errForbidden marks a 403 from the security/scheme layer. It signals a deployment
// fault that a newcode cannot fix, so it is never final; Run reports it and retries.
var errForbidden = errors.New("host_request_forbidden")

// finalRefusal is the allowlist of activation refusals, from hosts.ex and
// agent_ingress.ex: 401 unauthorized_host (revoked or unknown host/fingerprint, not an
// activation-state identity), 400 identity_predecessor_expired, 400 identity_predecessor_missing.
func finalRefusal(status int, code string) bool {
	switch {
	case status == http.StatusUnauthorized:
		return code == "unauthorized_host"
	case status == http.StatusBadRequest:
		return code == "identity_predecessor_expired" || code == "identity_predecessor_missing"
	}
	return false
}

// IsFinalRefusal reports whether err is a final (allowlisted) activation refusal.
func IsFinalRefusal(err error) bool { return errors.Is(err, errRefused) }

func errorCode(raw []byte) string {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return body.Error.Code
}

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
	if response.StatusCode == http.StatusForbidden {
		return nil, errForbidden
	}
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		if finalRefusal(response.StatusCode, errorCode(body)) {
			return nil, errRefused
		}
		return nil, ErrTransport
	}
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
		return transportOnly(e)
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

// transportOnly hides a refusal outside activation, which is never terminal.
func transportOnly(e error) error {
	if errors.Is(e, errRefused) || errors.Is(e, errForbidden) {
		return ErrTransport
	}
	return e
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
			return transportOnly(e)
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
		return e // errRefused stays distinguishable for Run
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

// Seams for tests; production values are Renew and session.
var (
	renewFn   = Renew
	sessionFn = session
	runNow    = time.Now
	// renewRetry bounds a session on a credential whose renewal is due but failed.
	renewRetry = 30 * time.Second
)

// activationRetry is local to one Run: restarting still replays pending immediately.
// The key contains only issued public identity material, never private keys.
type activationRetry struct {
	key   [3]string
	delay time.Duration
	next  time.Time
}

func (r *activationRetry) sync(state *identity.State) {
	var key [3]string
	if state.Pending != nil && state.Pending.Certificate != "" {
		key = [3]string{state.Pending.IdentityID, state.Pending.Certificate, state.Pending.IssuerCertificate}
	}
	if key != r.key || key == [3]string{} {
		*r = activationRetry{key: key}
	}
}

func (r *activationRetry) failed(now, expires time.Time, activeValid bool) time.Time {
	if r.key == [3]string{} || !activeValid || !expires.After(now.Add(6*time.Hour)) {
		r.delay = renewRetry
	} else if r.delay == 0 {
		r.delay = renewRetry
	} else {
		r.delay *= 2
		if r.delay > 30*time.Minute {
			r.delay = 30 * time.Minute
		}
	}
	r.next = now.Add(r.delay)
	if activeValid {
		r.next = sessionDeadline(now, expires, r.next)
	}
	return r.next
}

// sessionDeadline ends an idle socket at renewal time (six hours before expiry),
// or at actual expiry when renewal is already due, and never later than limit.
func sessionDeadline(now, expires, limit time.Time) time.Time {
	deadline := expires.Add(-6 * time.Hour)
	if !deadline.After(now) {
		deadline = expires
	}
	if !limit.IsZero() && limit.Before(deadline) {
		deadline = limit
	}
	return deadline
}

func session(ctx context.Context, state *identity.State, limit time.Time, onEvent func(Event)) error {
	expires, e := identity.Expires(state)
	if e != nil {
		return ErrTransport
	}
	deadline := sessionDeadline(time.Now(), expires, limit)
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
	var activation activationRetry
	for {
		if ctx.Err() != nil {
			return nil
		}
		state, e := identity.Load(dir)
		if e != nil || state.Active == nil {
			return ErrTransport
		}
		if identity.Expired(state) && (state.Pending == nil || state.Pending.Certificate == "" || identity.PendingExpired(state)) {
			// Staging needs a valid predecessor and activation a valid pending
			// credential, so retrying is futile.
			return ErrRecoveryRequired
		}
		activation.sync(state)
		now := runNow()
		var limit time.Time
		expires, e := identity.Expires(state)
		pending := state.Pending != nil && state.Pending.Certificate != ""
		needsRenewal := pending || (e == nil && !expires.After(now.Add(6*time.Hour)))
		if needsRenewal && (e != nil || activation.next.IsZero() || !now.Before(activation.next)) {
			renewErr := renewFn(ctx, dir)
			if renewErr == nil {
				if state, e = identity.Load(dir); e != nil || state.Active == nil {
					return ErrTransport
				}
				activation = activationRetry{}
			} else {
				if errors.Is(renewErr, errForbidden) {
					// Security/scheme faults are retryable deployment faults, not recovery.
					emit(onEvent, "activation_forbidden", state.HostID)
				} else if identity.Expired(state) && errors.Is(renewErr, errRefused) {
					return ErrRecoveryRequired
				}
				// Staging can have saved an issued pending credential before activation
				// failed; bind the retry to that newly persisted identity too.
				state, e = identity.Load(dir)
				if e != nil || state.Active == nil {
					return ErrTransport
				}
				activation.sync(state)
				expires, e = identity.Expires(state)
				limit = activation.failed(runNow(), expires, e == nil)
			}
			e = nil
		} else if needsRenewal {
			limit = sessionDeadline(now, expires, activation.next)
		}

		if e == nil {
			e = sessionFn(ctx, state, limit, func(event Event) { attempt = 0; emit(onEvent, event.Kind, event.HostID) })
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
