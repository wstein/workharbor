// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
)

type streamPort interface {
	InvokeStream(context.Context, Call, io.Reader, io.Writer) error
}

// ForwardStream shares the same scope/capability/approval admission as Forward.
// Only the reconstructed typed union reaches the native port. No raw executor.
func (g *Gateway) ForwardStream(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	c, e := g.prepareCall(ctx, args)
	if e != nil {
		return e
	}
	if port, ok := g.Port.(streamPort); ok {
		return port.InvokeStream(ctx, c, in, out)
	}
	if c.Kind == "run" || c.Kind == "events" || c.Kind == "exec" {
		return fail("provider_unavailable")
	}
	raw, e := g.Port.Invoke(ctx, c)
	if e != nil {
		return e
	}
	_, e = out.Write(raw)
	return e
}

// DriverServer is private local CLI plumbing, not a host/control-plane transport.
// Its socket and arguments confer no authority beyond Gateway's native fence.
type DriverServer struct {
	Socket   string
	root     string
	server   *http.Server
	cancel   context.CancelFunc
	once     sync.Once
	mu       sync.Mutex
	closed   bool
	active   sync.WaitGroup
	err      error
	requests map[string]*driverRequest
}

func (d *DriverServer) Close() error {
	d.once.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.mu.Unlock()
		d.cancel()
		d.mu.Lock()
		records := []*driverRequest{}
		for _, r := range d.requests {
			records = append(records, r)
		}
		d.mu.Unlock()
		for _, r := range records {
			d.abort(r)
		}
		d.server.Close()
		d.active.Wait()
		d.err = os.RemoveAll(d.root)
	})
	return d.err
}

type flushWriter struct{ w http.ResponseWriter }

func (w flushWriter) Write(p []byte) (int, error) {
	n, e := w.w.Write(p)
	if e == nil {
		e = http.NewResponseController(w.w).Flush()
	}
	return n, e
}
func driverArgs(body io.Reader) ([]string, error) {
	var header [4]byte
	if _, e := io.ReadFull(body, header[:]); e != nil {
		return nil, fail("provider_refused")
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > metadataLimit {
		return nil, fail("provider_refused")
	}
	raw := make([]byte, length)
	if _, e := io.ReadFull(body, raw); e != nil {
		return nil, fail("provider_refused")
	}
	value, e := wirejson.Parse(raw)
	if e != nil {
		return nil, fail("provider_refused")
	}
	m, ok := value.(map[string]any)
	if !ok || !exactKeys(m, []string{"args"}) {
		return nil, fail("provider_refused")
	}
	values, ok := m["args"].([]any)
	if !ok {
		return nil, fail("provider_refused")
	}
	args := make([]string, len(values))
	for i, v := range values {
		arg, ok := v.(string)
		if !ok {
			return nil, fail("provider_refused")
		}
		args[i] = arg
	}
	return args, nil
}
func StartDriver(ctx context.Context, g *Gateway, root string) (*DriverServer, error) {
	if g == nil || g.Authorize == nil {
		return nil, fail("forbidden")
	}
	return startDriver(ctx, root, g.Authorize, g.ForwardStream)
}
func startDriver(ctx context.Context, root string, authorize func(context.Context) error, forward func(context.Context, []string, io.Reader, io.Writer) error) (*DriverServer, error) {
	if authorize == nil || forward == nil {
		return nil, fail("forbidden")
	}
	if e := authorize(ctx); e != nil {
		return nil, e
	}
	if _, ok := ctx.Deadline(); !ok || !ownedDirectory(root) {
		return nil, fail("provider_refused")
	}
	private, e := os.MkdirTemp(root, "driver-")
	if e != nil {
		return nil, fail("provider_unavailable")
	}
	socket := filepath.Join(private, "driver.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		os.RemoveAll(private)
		return nil, fail("provider_unavailable")
	}
	if os.Chmod(socket, 0600) != nil || !ownedSocket(socket) {
		listener.Close()
		os.RemoveAll(private)
		return nil, fail("provider_unavailable")
	}
	child, cancel := context.WithCancel(ctx)
	d := &DriverServer{Socket: socket, root: private, cancel: cancel, requests: map[string]*driverRequest{}}
	server := &http.Server{BaseContext: func(net.Listener) context.Context { return child }, ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		return context.WithValue(ctx, driverConnectionKey{}, c)
	}}
	d.server = server
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			http.Error(w, "provider_refused", http.StatusServiceUnavailable)
			return
		}
		d.active.Add(1)
		d.mu.Unlock()
		defer d.active.Done()
		if r.Method != "POST" || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.Host != "workharbor-driver" || r.Header.Get("Content-Type") != "application/octet-stream" {
			http.Error(w, "unknown", 400)
			return
		}
		token := r.Header.Get("WorkHarbor-Request")
		if !hexID.MatchString(token) {
			http.Error(w, "unknown", 400)
			return
		}
		if r.URL.Path == "/register" {
			d.mu.Lock()
			if d.closed || d.requests[token] != nil {
				d.mu.Unlock()
				http.Error(w, "unknown", 409)
				return
			}
			recordCtx, recordCancel := context.WithCancel(child)
			d.requests[token] = &driverRequest{ctx: recordCtx, cancel: recordCancel, done: make(chan struct{})}
			d.mu.Unlock()
			io.WriteString(w, "registered")
			return
		}
		if r.URL.Path == "/cancel" {
			d.mu.Lock()
			record := d.requests[token]
			if record != nil && !record.started {
				record.cancel()
				record.once.Do(func() { close(record.done) })
			}
			d.mu.Unlock()
			if record == nil {
				io.WriteString(w, "unknown")
				return
			}
			d.abort(record)
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case <-record.done:
				io.WriteString(w, "client_complete")
			case <-timer.C:
				io.WriteString(w, "unknown")
			case <-r.Context().Done():
				return
			}
			return
		}
		if r.URL.Path != "/invoke" {
			http.Error(w, "unknown", 400)
			return
		}
		d.mu.Lock()
		record := d.requests[token]
		if d.closed || record == nil || record.started || record.ctx.Err() != nil || r.Context().Err() != nil {
			d.mu.Unlock()
			http.Error(w, "unknown", 409)
			return
		}
		connection, _ := r.Context().Value(driverConnectionKey{}).(net.Conn)
		if connection == nil {
			d.mu.Unlock()
			http.Error(w, "unknown", 409)
			return
		}
		record.started = true
		record.body = r.Body
		record.connection = connection
		d.mu.Unlock()
		stop := context.AfterFunc(r.Context(), func() { d.abort(record) })
		defer func() {
			stop()
			record.cancel()
			d.mu.Lock()
			delete(d.requests, token)
			record.once.Do(func() { close(record.done) })
			d.mu.Unlock()
		}()
		w.Header().Set("Trailer", "WorkHarbor-Result")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("WorkHarbor-Result", "refused")
		if http.NewResponseController(w).EnableFullDuplex() != nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		args, e := driverArgs(r.Body)
		if e != nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		if e = record.ctx.Err(); e == nil {
			e = authorize(record.ctx)
		}
		if e == nil {
			e = forward(record.ctx, args, r.Body, flushWriter{w})
		}
		if e == nil && record.ctx.Err() == nil {
			w.Header().Set("WorkHarbor-Result", "ok")
		}

	})
	go func() { server.Serve(listener) }()
	go func() { <-child.Done(); d.Close() }()
	return d, nil
}

// ProxyDocker is the product-owned executable adapter's private socket client.
// The first bounded frame carries argv; the remaining body is container stdin.
// HTTP is used only over this owned Unix socket, with no proxies or redirects.
func ProxyDocker(ctx context.Context, socket string, args []string, in io.Reader, out io.Writer) error {
	if _, ok := ctx.Deadline(); !ok || !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || !ownedSocket(socket) {
		return fail("provider_refused")
	}
	// A streaming input must be owned/closable so transport cancellation cannot
	// leave net/http's request writer blocked in an arbitrary caller reader.
	if in != nil {
		switch in.(type) {
		case io.ReadCloser, *bytes.Reader, *bytes.Buffer, *strings.Reader:
		default:
			return fail("provider_refused")
		}
	}
	for _, arg := range args {
		if !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
			return fail("provider_refused")
		}
	}
	var tokenBytes [32]byte
	if _, e := rand.Read(tokenBytes[:]); e != nil {
		return fail("provider_unavailable")
	}
	token := hex.EncodeToString(tokenBytes[:])
	raw, e := json.Marshal(map[string]any{"args": args})
	if e != nil || len(raw) == 0 || len(raw) > metadataLimit {
		return fail("provider_refused")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	if in == nil {
		in = bytes.NewReader(nil)
	}
	closeInput := func() {
		if closer, ok := in.(io.Closer); ok {
			closer.Close()
		}
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		connection, e := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if e != nil {
			return nil, e
		}
		return &proxyConnection{Conn: connection, closeInput: closeInput}, nil
	}}
	controlTransport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer controlTransport.CloseIdleConnections()
	controlClient := &http.Client{Transport: controlTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	control := func(controlCtx context.Context, path string) string {
		request, e := http.NewRequestWithContext(controlCtx, "POST", "http://workharbor-driver"+path, nil)
		if e != nil {
			return "unknown"
		}
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("WorkHarbor-Request", token)
		response, e := controlClient.Do(request)
		if e != nil {
			return "unknown"
		}
		defer response.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(response.Body, 33))
		if e != nil || response.StatusCode != 200 {
			return "unknown"
		}
		return string(raw)
	}
	if control(ctx, "/register") != "registered" {
		return fail("provider_unavailable")
	}
	cancelDone := make(chan struct{})
	var cancelOnce sync.Once
	abort := func() {
		cancelOnce.Do(func() {
			if closer, ok := in.(io.Closer); ok {
				closer.Close()
			}
			child, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			control(child, "/cancel")
			close(cancelDone)
		})
	}
	stopCancel := context.AfterFunc(ctx, abort)
	normal := false
	defer func() {
		stopCancel()
		if !normal || ctx.Err() != nil {
			abort()
			<-cancelDone
		}
	}()
	if ctx.Err() != nil {
		return fail("provider_unavailable")
	}
	body := &proxyRequestBody{Reader: io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(raw), in)}
	if closer, ok := in.(io.Closer); ok {
		body.input = closer
	}
	request, e := http.NewRequestWithContext(ctx, "POST", "http://workharbor-driver/invoke", body)
	if e != nil {
		return fail("provider_refused")
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("WorkHarbor-Request", token)
	response, e := client.Do(request)
	if e != nil {
		return fail("provider_unavailable")
	}
	defer func() {
		if !normal || ctx.Err() != nil {
			abort()
			<-cancelDone
		}
		response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return fail("provider_refused")
	}
	if _, e = io.Copy(out, response.Body); e != nil {
		return fail("provider_unavailable")
	}
	if response.Trailer.Get("WorkHarbor-Result") != "ok" {
		return fail("provider_refused")
	}
	if ctx.Err() != nil {
		return fail("provider_unavailable")
	}
	normal = true
	return nil
}

type driverConnectionKey struct{}
type driverRequest struct {
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	once       sync.Once
	started    bool
	body       io.ReadCloser
	connection net.Conn
}

func (d *DriverServer) abort(record *driverRequest) {
	record.cancel()
	d.mu.Lock()
	connection, body := record.connection, record.body
	d.mu.Unlock()
	// Closing only this request's dedicated private connection unblocks body.Close.
	// No Docker socket/resource is controlled by this transport cancellation.
	if connection != nil {
		connection.Close()
	}
	if body != nil {
		body.Close()
	}
}

// net/http must be able to interrupt its own request writer when the exact
// server connection disappears before response headers, even without ctx cancel.
type proxyRequestBody struct {
	io.Reader
	input io.Closer
}

func (b *proxyRequestBody) Close() error {
	if b.input != nil {
		return b.input.Close()
	}
	return nil
}

// A peer close must unblock the request writer's owned stdin even while
// net/http is waiting for that writer before returning a response-header error.
type proxyConnection struct {
	net.Conn
	closeInput func()
}

func (c *proxyConnection) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	if e != nil {
		c.closeInput()
	}
	return n, e
}
func (c *proxyConnection) Close() error { c.closeInput(); return c.Conn.Close() }
