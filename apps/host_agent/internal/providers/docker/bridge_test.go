// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type streamingEngine struct {
	*recordingEngine
	started, canceled chan struct{}
}

func (e *streamingEngine) InvokeStream(ctx context.Context, c Call, in io.Reader, out io.Writer) error {
	if c.Kind != "exec" {
		return fail("provider_refused")
	}
	e.effects = append(e.effects, c)
	io.WriteString(out, "ready\n")
	close(e.started)
	<-ctx.Done()
	close(e.canceled)
	return ctx.Err()
}

type notifyWriter struct {
	buffer bytes.Buffer
	ready  chan struct{}
}

func (w *notifyWriter) Write(p []byte) (int, error) {
	n, e := w.buffer.Write(p)
	select {
	case <-w.ready:
	default:
		close(w.ready)
	}
	return n, e
}
func bridgeRoot(t *testing.T) string {
	t.Helper()
	base, e := filepath.EvalSymlinks("/tmp")
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(base, "whr-driver-")
	if e == nil {
		t.Cleanup(func() { os.RemoveAll(root) })
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		t.Fatal(e)
	}
	if os.Chmod(root, 0700) != nil {
		t.Fatal("private root")
	}
	return root
}
func TestPrivateDriverBridgeStreamsBeforeExitAndCancelsExactClient(t *testing.T) {
	g, port := gatewayFixture(t, Hooks)
	stream := &streamingEngine{recordingEngine: port, started: make(chan struct{}), canceled: make(chan struct{})}
	g.Port = stream
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	driver, e := StartDriver(ctx, g, bridgeRoot(t))
	if e != nil {
		t.Fatal(e)
	}
	defer driver.Close()
	child, cancel := context.WithCancel(ctx)
	out := &notifyWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- ProxyDocker(child, driver.Socket, []string{"exec", "-i", "-u", "1000:1000", g.Target.ContainerID, "true"}, strings.NewReader(""), out)
	}()
	select {
	case <-out.ready:
	case <-ctx.Done():
		t.Fatal("stream buffered until exit")
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("proxy did not reap client")
	}
	select {
	case <-stream.canceled:
	case <-ctx.Done():
		t.Fatal("disconnect did not cancel exact native command")
	}
	if out.buffer.String() != "ready\n" || len(port.effects) != 1 {
		t.Fatal("stream changed")
	}
}
func TestPrivateDriverBridgeRejectsUnknownArgvBeforeInvoke(t *testing.T) {
	g, port := gatewayFixture(t, Hooks)
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	driver, e := StartDriver(ctx, g, bridgeRoot(t))
	if e != nil {
		t.Fatal(e)
	}
	defer driver.Close()
	var out bytes.Buffer
	e = ProxyDocker(ctx, driver.Socket, []string{"--host", "unix:///foreign.sock", "exec"}, strings.NewReader(""), &out)
	if e == nil || out.Len() != 0 || len(port.effects) != 0 {
		t.Fatal("unknown args forwarded", e)
	}
}

type instantEngine struct{ *recordingEngine }

func (e *instantEngine) InvokeStream(_ context.Context, c Call, _ io.Reader, _ io.Writer) error {
	e.effects = append(e.effects, c)
	return nil
}
func TestPrivateDriverNeverNormalizesInvalidArgumentBytes(t *testing.T) {
	g, port := gatewayFixture(t, Hooks)
	g.Port = &instantEngine{port}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	driver, e := StartDriver(ctx, g, bridgeRoot(t))
	if e != nil {
		t.Fatal(e)
	}
	defer driver.Close()
	var out bytes.Buffer
	e = ProxyDocker(ctx, driver.Socket, []string{"exec", "-i", "-u", "1000:1000", g.Target.ContainerID, "bad\xff"}, strings.NewReader(""), &out)
	if e == nil || len(port.effects) != 0 {
		t.Fatal("invalid bytes normalized into executable command", e)
	}
}
