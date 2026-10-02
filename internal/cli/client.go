// Package cli is the whr command line (design §9.1, D14, D37): cobra commands
// that talk to the supervisor's JSON API, plus `serve`. Stdout carries data only
// (a table or, with --json, the API's envelope); every message for a human goes
// to stderr; exit codes come from internal/exitcode (design §9.2).
package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/exitcode"
)

// RemoteError is an error envelope from the server. It carries the server's
// exit code, so a script sees the same number from the API and from whr.
type RemoteError struct {
	Code    string
	Exit    int
	Message string
}

func (e *RemoteError) Error() string { return e.Message }

// ExitCode implements exitcode.Coder.
func (e *RemoteError) ExitCode() int { return e.Exit }

// connError is a server that cannot be reached.
type connError struct{ err error }

func (e connError) Error() string {
	return "cannot reach the supervisor (is `whr serve` running?): " + e.err.Error()
}
func (e connError) Unwrap() error { return e.err }

// usageError is a command line the user got wrong. It is exit code 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }
func (usageError) ExitCode() int   { return exitcode.Usage }

// Client talks to the supervisor's API. The token is held for the requests and
// never printed, logged or put in an error.
type Client struct {
	base  string
	token string
	hc    *http.Client
	// dial opens a connection to the supervisor, for a stream (Upgrade). Nil
	// means TCP to the host of base, as in tests.
	dial func(ctx context.Context) (net.Conn, error)
}

// ClientConfig is the part of the configuration file a client needs. Listen is the
// web UI's address (only `whr github app create` uses it): the API is on a unix
// socket in the state directory (D29, §7.5).
type ClientConfig struct {
	Listen       string `json:"listen"`
	APITokenFile string `json:"api_token_file"`
	StateDir     string `json:"state_dir"`
}

// ReadClientConfig reads listen, api_token_file and state_dir from the
// configuration file. Other keys are not checked here: `whr serve` validates the
// whole file.
func ReadClientConfig(path string) (ClientConfig, error) {
	f, err := os.Open(path) //nolint:gosec // the user names their own configuration file
	if err != nil {
		return ClientConfig{}, fmt.Errorf("configuration: %w", err)
	}
	defer func() { _ = f.Close() }()
	var c ClientConfig
	if err := json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&c); err != nil {
		return ClientConfig{}, fmt.Errorf("configuration %s: %w", path, err)
	}
	if c.APITokenFile == "" {
		return ClientConfig{}, fmt.Errorf("configuration %s: api_token_file is needed", path)
	}
	return c, nil
}

// NewClient builds a client from the configuration file: the API's unix socket in
// the state directory and the token read with config.ReadSecret (a 0600 file, never
// a command line or the environment). The API is served only on that socket (D29,
// §7.5), so the token never crosses a network, not even loopback.
func NewClient(configPath string) (*Client, error) {
	cc, err := ReadClientConfig(configPath)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	sock := config.APISocketPath(cc.StateDir, home)
	tok, err := config.ReadSecret(cc.APITokenFile)
	if err != nil {
		return nil, err
	}
	return newSocketClient(sock, strings.TrimSpace(string(tok))), nil
}

// newSocketClient is a client that dials a unix socket whatever the URL says.
func newSocketClient(sock, token string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}
	dial := func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}
	return &Client{base: "http://whr", token: token, hc: &http.Client{Timeout: 30 * time.Second, Transport: tr}, dial: dial}
}

// NewClientFor is for tests and wiring that already know the address and token.
func NewClientFor(base, token string) *Client {
	return &Client{base: base, token: token, hc: &http.Client{Timeout: 30 * time.Second}}
}

// DefaultConfigPath is where the configuration file is looked for: --config,
// then $WHR_CONFIG, then ~/.config/whr/config.json.
func DefaultConfigPath(getenv func(string) string) string {
	if p := getenv("WHR_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(getenv("HOME"), ".config", "whr", "config.json")
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code     string `json:"code"`
		ExitCode int    `json:"exit_code"`
		Message  string `json:"message"`
	} `json:"error"`
}

// Do sends a request and returns the raw body of the envelope (for --json) and
// its data. An error envelope is a *RemoteError with the server's exit code.
func (c *Client) Do(ctx context.Context, method, path string, body any, idemKey string) (raw, data []byte, err error) {
	return c.do(ctx, c.hc, method, path, body, idemKey)
}

// slowTimeout bounds a request that builds an image or an environment: it takes
// minutes, where an ordinary request is done in seconds.
const slowTimeout = 30 * time.Minute

// DoSlow is Do for a request that can take minutes (an image is built, an
// environment is made or replaced). The ordinary client's 30 seconds would end the
// request while the supervisor works, and the work would be cancelled with it.
func (c *Client) DoSlow(ctx context.Context, method, path string, body any, idemKey string) (raw, data []byte, err error) {
	hc := *c.hc
	hc.Timeout = slowTimeout
	return c.do(ctx, &hc, method, path, body, idemKey)
}

func (c *Client) do(ctx context.Context, hc *http.Client, method, path string, body any, idemKey string) (raw, data []byte, err error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, connError{redactURLError(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, nil, err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, fmt.Errorf("the server answered %d with something that is not an envelope", resp.StatusCode)
	}
	if !env.OK {
		if env.Error == nil {
			return raw, nil, fmt.Errorf("the server answered %d without an error", resp.StatusCode)
		}
		return raw, nil, &RemoteError{Code: env.Error.Code, Exit: env.Error.ExitCode, Message: env.Error.Message}
	}
	return raw, env.Data, nil
}

// redactURLError drops the URL of a failed request from its error, which would
// otherwise print the address and nothing the user can use.
func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// Stream opens a server-sent event stream. The caller closes the body.
func (c *Client) Stream(ctx context.Context, path, lastEventID string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	hc := &http.Client{Transport: c.hc.Transport} // no timeout: the stream is open until the user stops it
	resp, err := hc.Do(req)
	if err != nil {
		return nil, connError{redactURLError(err)}
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var env envelope
		if json.Unmarshal(raw, &env) == nil && env.Error != nil {
			return nil, &RemoteError{Code: env.Error.Code, Exit: env.Error.ExitCode, Message: env.Error.Message}
		}
		return nil, fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	return resp, nil
}

// Upgrade opens a stream on the supervisor: it sends the request with the token
// and an Upgrade header and, on 101 Switching Protocols, returns the connection
// and a reader on it, which may already hold bytes the server sent after the
// headers. Any other answer is the ordinary error envelope. The caller closes the
// connection. The token goes over the private socket NewClient dials.
func (c *Client) Upgrade(ctx context.Context, path, protocol string) (net.Conn, *bufio.Reader, error) {
	dial := c.dial
	if dial == nil {
		u, err := url.Parse(c.base)
		if err != nil {
			return nil, nil, err
		}
		dial = func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", u.Host)
		}
	}
	conn, err := dial(ctx)
	if err != nil {
		return nil, nil, connError{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", protocol)
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, nil, connError{err}
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, nil, connError{err}
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		defer func() { _ = conn.Close(); _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var env envelope
		if json.Unmarshal(raw, &env) == nil && env.Error != nil {
			return nil, nil, &RemoteError{Code: env.Error.Code, Exit: env.Error.ExitCode, Message: env.Error.Message}
		}
		return nil, nil, fmt.Errorf("the server answered %d instead of switching protocols", resp.StatusCode)
	}
	return conn, br, nil
}
