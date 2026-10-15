// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func privateControl(t *testing.T, ctx context.Context, socket, path, token string) string {
	t.Helper()
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	request, e := http.NewRequestWithContext(ctx, "POST", "http://workharbor-driver"+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("WorkHarbor-Request", token)
	response, e := (&http.Client{Transport: transport}).Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, 64))
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestPrivateDriverCancellationRegistrationIsOneUseAndUnknownStaysUnknown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	effects := 0
	driver, e := startDriver(ctx, bridgeRoot(t), func(context.Context) error { return nil }, func(context.Context, []string, io.Reader, io.Writer) error { effects++; return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer driver.Close()
	token := strings.Repeat("a", 64)
	if got := privateControl(t, ctx, driver.Socket, "/cancel", token); got != "unknown" {
		t.Fatal(got)
	}
	if got := privateControl(t, ctx, driver.Socket, "/register", token); got != "registered" {
		t.Fatal(got)
	}
	if got := privateControl(t, ctx, driver.Socket, "/register", token); got == "registered" {
		t.Fatal("duplicate registration")
	}
	if got := privateControl(t, ctx, driver.Socket, "/cancel", token); got != "client_complete" {
		t.Fatal(got)
	}
	if got := privateControl(t, ctx, driver.Socket, "/invoke", token); got == "ok" || effects != 0 {
		t.Fatal("canceled unstarted token invoked", got, effects)
	}
}
func TestPrivateDriverUnreadPipeCancellationAndCloseReapExactHandler(t *testing.T) {
	for _, closeServer := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "close"}[closeServer], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			started, complete := make(chan struct{}), make(chan struct{})
			driver, e := startDriver(ctx, bridgeRoot(t), func(ctx context.Context) error { return ctx.Err() }, func(ctx context.Context, _ []string, in io.Reader, out io.Writer) error {
				close(started)
				out.Write([]byte("ready\n"))
				<-ctx.Done()
				_, readError := io.Copy(io.Discard, in)
				close(complete)
				return readError
			})
			if e != nil {
				t.Fatal(e)
			}
			defer driver.Close()
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			child, stop := context.WithCancel(ctx)
			defer stop()
			done := make(chan error, 1)
			var output bytes.Buffer
			go func() { done <- ProxyDocker(child, driver.Socket, []string{"-v"}, input, &output) }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("not started")
			}
			if closeServer {
				if e := driver.Close(); e != nil {
					t.Fatal(e)
				}
			} else {
				stop()
			}
			select {
			case <-complete:
			case <-ctx.Done():
				t.Fatal("owned unread input did not unblock/reap")
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("cancellation succeeded")
				}
			case <-ctx.Done():
				t.Fatal("client still blocked")
			}
		})
	}
}
