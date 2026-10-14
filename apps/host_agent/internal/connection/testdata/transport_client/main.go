// SPDX-License-Identifier: EUPL-1.2
// Independent bounded test driver. Inputs are private fixture files, never argv secrets.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/connection"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/identity"
)

type input struct {
	Dir, Endpoint, ServerCA, Code              string
	Certificate, PrivateKey, IssuerCertificate string
	Pending                                    bool
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "fixture_failed")
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 3 {
		return connection.ErrTransport
	}
	raw, e := os.ReadFile(os.Args[2])
	if e != nil {
		return e
	}
	var in input
	if e = json.Unmarshal(raw, &in); e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	switch os.Args[1] {
	case "boundary", "fragment", "oversize", "wrong_scope", "wrong_version", "duplicate", "binary", "invalid_utf8":
		return wireProbe(ctx, in, os.Args[1])
	case "probe_cert":
		cfg, e := identity.TLSConfig(&identity.State{ServerCAFile: in.ServerCA}, false)
		if e != nil {
			return e
		}
		leaf, e := base64.StdEncoding.DecodeString(in.Certificate)
		if e != nil {
			return e
		}
		ca, e := base64.StdEncoding.DecodeString(in.IssuerCertificate)
		if e != nil {
			return e
		}
		keyDER, e := base64.StdEncoding.DecodeString(in.PrivateKey)
		if e != nil {
			return e
		}
		key, e := x509.ParsePKCS8PrivateKey(keyDER)
		if e != nil {
			return e
		}
		cert := tls.Certificate{Certificate: [][]byte{leaf, ca}, PrivateKey: key}
		// Force the negative fixture certificate even when the server's CA list would
		// otherwise make the client omit it. Server verification remains mandatory.
		var presented atomic.Bool
		cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { presented.Store(true); return &cert, nil }
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: false, Proxy: nil}, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return connection.ErrTransport }}
		defer c.CloseIdleConnections()
		ws, response, e := websocket.Dial(ctx, "wss"+strings.TrimPrefix(in.Endpoint, "https")+"/agent/v1/connect", &websocket.DialOptions{HTTPClient: c, CompressionMode: websocket.CompressionDisabled})
		if e == nil {
			ws.CloseNow()
			return connection.ErrTransport
		}
		if !presented.Load() {
			return connection.ErrTransport
		}
		if response != nil && response.StatusCode == 401 {
			fmt.Println("server_refused")
			return nil
		}
		if strings.Contains(e.Error(), "remote error: tls: unknown certificate authority") {
			fmt.Println("server_tls_unknown_ca")
			return nil
		}
		if strings.Contains(e.Error(), "remote error: tls: expired certificate") {
			fmt.Println("server_tls_expired")
			return nil
		}
		return connection.ErrTransport
	case "tls_preflight":
		ca, e := os.ReadFile(in.ServerCA)
		if e != nil {
			return e
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return connection.ErrTransport
		}
		peer, e := os.ReadFile(filepath.Join(in.Dir, "server.pem"))
		if e != nil {
			return e
		}
		block, _ := pem.Decode(peer)
		if block == nil {
			return connection.ErrTransport
		}
		cert, e := x509.ParseCertificate(block.Bytes)
		if e != nil {
			return e
		}
		if _, e = cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost"}); e != nil {
			return e
		}
		if _, e = cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "foreign.test"}); e == nil {
			return connection.ErrTransport
		}
		fmt.Println("server_trust_preflight_ok")
		return nil
	case "diagnose":
		state, e := identity.Load(in.Dir)
		if e != nil {
			fmt.Println("state_refused")
			return nil
		}
		cfg, e := identity.TLSConfig(state, false)
		if e != nil {
			_, systemErr := x509.SystemCertPool()
			caBytes, fileErr := os.ReadFile(state.ServerCAFile)
			roots := x509.NewCertPool()
			block, _ := pem.Decode(caBytes)
			class := "no_pem_block"
			if block != nil {
				_, parseErr := x509.ParseCertificate(block.Bytes)
				class = "parsed"
				if parseErr != nil {
					class = "other_parse_failure"
					for _, known := range []string{"x509: negative serial number", "x509: malformed serial number", "x509: malformed certificate", "x509: malformed extensions", "x509: invalid RDNSequence", "x509: malformed validity", "x509: malformed time"} {
						if parseErr.Error() == known {
							class = strings.ReplaceAll(known, " ", "_")
						}
					}
				}
			}
			fmt.Printf("tls_config_refused_system_ok_%t_explicit_file_ok_%t_explicit_pem_ok_%t_bytes_%d_class_%s\n", systemErr == nil, fileErr == nil, roots.AppendCertsFromPEM(caBytes), len(caBytes), class)
			return nil
		}
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: false, Proxy: nil}, Timeout: 3 * time.Second}
		defer c.CloseIdleConnections()
		req, _ := http.NewRequestWithContext(ctx, "GET", in.Endpoint+"/agent/v1/connect", nil)
		response, e := c.Do(req)
		if e != nil {
			var ca x509.UnknownAuthorityError
			var name x509.HostnameError
			var invalid x509.CertificateInvalidError
			switch {
			case errors.As(e, &ca):
				fmt.Println("tls_unknown_authority")
			case errors.As(e, &name):
				fmt.Println("tls_wrong_name")
			case errors.As(e, &invalid):
				fmt.Printf("tls_invalid_certificate_%d\n", invalid.Reason)
			default:
				fmt.Printf("transport_failure_type_%T\n", e)
			}
			return nil
		}
		defer response.Body.Close()
		fmt.Printf("http_status_%d\n", response.StatusCode)
		return nil
	case "enroll":
		if _, e = identity.Prepare(in.Dir, in.Endpoint, in.ServerCA); e != nil {
			return e
		}
		if e = connection.Enroll(ctx, in.Dir, in.Code); e != nil {
			return e
		}
		fmt.Println("enrolled")
		return nil
	case "renew":
		if e = connection.Renew(ctx, in.Dir); e != nil {
			return e
		}
		fmt.Println("renewed")
		return nil
	case "once", "run":
		once := os.Args[1] == "once"
		return connection.Run(ctx, in.Dir, func(event connection.Event) {
			if event.Kind == "connected" {
				fmt.Println("connected")
				if once {
					cancel()
				}
			} else {
				fmt.Println("disconnected")
			}
		})
	case "stage_lost", "stage_only":
		state, e := identity.Load(in.Dir)
		if e != nil {
			return e
		}
		if e = identity.PrepareRenewal(in.Dir, state); e != nil {
			return e
		}
		raw, e = post(ctx, state, false, "/agent/v1/identity/renew", map[string]any{"schema_version": 1, "csr": state.Pending.CSR})
		if e != nil {
			return e
		}
		var response identity.Response
		if e = json.Unmarshal(raw, &response); e != nil {
			return e
		}
		if e = identity.AcceptPending(in.Dir, state, response); e != nil {
			return e
		}
		if os.Args[1] == "stage_only" {
			fmt.Println("staged")
			return nil
		}
		// Exercise real TLS and actual server promotion, then deliberately discard
		// the response without local promotion and exit the client process.
		if _, e = post(ctx, state, true, "/agent/v1/identity/activate", map[string]any{"schema_version": 1}); e != nil {
			return e
		}
		fmt.Println("lost_ack")
		os.Exit(73) // injected abrupt client crash before local promotion
		return nil
	case "probe", "idle":
		state, e := identity.Load(in.Dir)
		if e != nil {
			return e
		}
		cfg, e := identity.TLSConfig(state, in.Pending)
		if e != nil {
			if os.Args[1] == "probe" {
				fmt.Println("client_refused")
				return nil
			}
			return e
		}
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: false, Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return connection.ErrTransport }}
		defer c.CloseIdleConnections()
		ws, response, e := websocket.Dial(ctx, "wss"+strings.TrimPrefix(in.Endpoint, "https")+"/agent/v1/connect", &websocket.DialOptions{HTTPClient: c, CompressionMode: websocket.CompressionDisabled})
		if os.Args[1] == "probe" {
			if e == nil {
				ws.CloseNow()
				return connection.ErrTransport
			}
			if response == nil || response.StatusCode != 401 {
				return connection.ErrTransport
			}
			fmt.Println("server_refused")
			return nil
		}
		if e != nil {
			return e
		}
		defer ws.CloseNow()
		ws.SetReadLimit(4096)
		for _, kind := range []string{"hello", "heartbeat"} {
			m := map[string]any{"schema_version": 1, "kind": kind, "owner_id": state.OwnerID, "host_id": state.HostID}
			if kind == "hello" {
				m["capabilities"] = []string{}
			}
			data, _ := json.Marshal(m)
			if e = ws.Write(ctx, websocket.MessageText, data); e != nil {
				return e
			}
			typ, data, e := ws.Read(ctx)
			if e != nil || typ != websocket.MessageText {
				return connection.ErrTransport
			}
			ack, e := connection.DecodeMessage(data, state.OwnerID, state.HostID)
			if e != nil || ack["kind"] != kind+"_ack" {
				return connection.ErrTransport
			}
		}
		fmt.Println("ready")
		closed := make(chan error, 1)
		go func() { _, _, e := ws.Read(ctx); closed <- e }()
		ticks := time.NewTicker(5 * time.Second)
		defer ticks.Stop()
		for {
			select {
			case e = <-closed:
				if e == nil {
					return connection.ErrTransport
				}
				fmt.Println("closed")
				return nil
			case <-ticks.C:
				ping, cancelPing := context.WithTimeout(ctx, time.Second)
				_ = ws.Ping(ping)
				cancelPing()
			case <-ctx.Done():
				return connection.ErrTransport
			}
		}
	default:
		return connection.ErrTransport
	}
}
func post(ctx context.Context, state *identity.State, pending bool, path string, body any) ([]byte, error) {
	cfg, e := identity.TLSConfig(state, pending)
	if e != nil {
		return nil, e
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: false, Proxy: nil}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return connection.ErrTransport }}
	defer client.CloseIdleConnections()
	data, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, "POST", state.Endpoint+path, bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	r, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, connection.ErrTransport
	}
	return io.ReadAll(io.LimitReader(r.Body, 16385))
}

// Raw masked frames are independent negative test fixtures only; production uses Coder.
func wireProbe(ctx context.Context, in input, mode string) error {
	state, e := identity.Load(in.Dir)
	if e != nil {
		return e
	}
	cfg, e := identity.TLSConfig(state, false)
	if e != nil {
		return e
	}
	u, e := url.Parse(state.Endpoint)
	if e != nil {
		return e
	}
	sock, e := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", u.Host, cfg)
	if e != nil {
		return e
	}
	defer sock.Close()
	_ = sock.SetDeadline(time.Now().Add(5 * time.Second))
	key := make([]byte, 16)
	if _, e = rand.Read(key); e != nil {
		return e
	}
	request := "GET /agent/v1/connect HTTP/1.1\r\nHost: " + u.Host + "\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(key) + "\r\n\r\n"
	if _, e = io.WriteString(sock, request); e != nil {
		return e
	}
	reader := bufio.NewReader(sock)
	response, e := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if e != nil || response.StatusCode != 101 {
		return connection.ErrTransport
	}
	message := map[string]any{"schema_version": 1, "kind": "hello", "owner_id": state.OwnerID, "host_id": state.HostID, "capabilities": []string{}}
	opcode := byte(1)
	expected := uint16(1008)
	switch mode {
	case "wrong_scope":
		message["owner_id"] = state.HostID
	case "wrong_version":
		message["schema_version"] = 2
	case "binary":
		opcode = 2
	case "invalid_utf8":
		expected = 1007
	}
	raw, _ := json.Marshal(message)
	switch mode {
	case "boundary":
		raw = append(raw, bytes.Repeat([]byte(" "), 4096-len(raw))...)
	case "oversize":
		raw = append(raw, bytes.Repeat([]byte(" "), 4097-len(raw))...)
	case "duplicate":
		raw = append([]byte(`{"schema_version":1,`), raw[1:]...)
	case "invalid_utf8":
		raw = []byte{0xff}
	case "fragment":
		expected = 1009
		if e = writeFrame(sock, false, 1, bytes.Repeat([]byte(" "), 3000)); e != nil {
			return e
		}
		opcode = 0
		raw = bytes.Repeat([]byte(" "), 3000)
	}
	if e = writeFrame(sock, true, opcode, raw); e != nil {
		return e
	}
	code, payload, e := readFrame(reader)
	if e != nil {
		return e
	}
	if mode == "boundary" {
		if code != 1 {
			return connection.ErrTransport
		}
		m, e := connection.DecodeMessage(payload, state.OwnerID, state.HostID)
		if e != nil || m["kind"] != "hello_ack" {
			return connection.ErrTransport
		}
		fmt.Println("boundary_ack")
		return nil
	}
	if code != 8 || len(payload) < 2 || binary.BigEndian.Uint16(payload) != expected {
		return connection.ErrTransport
	}
	fmt.Printf("wire_refused_%d\n", expected)
	return nil
}
func writeFrame(w io.Writer, fin bool, opcode byte, data []byte) error {
	first := opcode
	if fin {
		first |= 0x80
	}
	header := []byte{first}
	if len(data) < 126 {
		header = append(header, 0x80|byte(len(data)))
	} else {
		header = append(header, 0x80|126, byte(len(data)>>8), byte(len(data)))
	}
	mask := make([]byte, 4)
	if _, e := rand.Read(mask); e != nil {
		return e
	}
	header = append(header, mask...)
	for n := range data {
		data[n] ^= mask[n%4]
	}
	_, e := w.Write(append(header, data...))
	return e
}
func readFrame(r io.Reader) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, e := io.ReadFull(r, header); e != nil {
		return 0, nil, e
	}
	length := int(header[1] & 0x7f)
	if header[1]&0x80 != 0 || length == 127 {
		return 0, nil, connection.ErrTransport
	}
	if length == 126 {
		b := make([]byte, 2)
		if _, e := io.ReadFull(r, b); e != nil {
			return 0, nil, e
		}
		length = int(binary.BigEndian.Uint16(b))
	}
	if length > 4096 {
		return 0, nil, connection.ErrTransport
	}
	payload := make([]byte, length)
	_, e := io.ReadFull(r, payload)
	return header[0] & 0xf, payload, e
}
