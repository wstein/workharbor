// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeProcessHelper(t *testing.T) {
	if os.Getenv("WORKHARBOR_NATIVE_PROCESS_HELPER") != "wait" {
		return
	}
	fmt.Fprint(os.Stdout, "ready\n")
	for {
		time.Sleep(time.Second)
	}
}
func TestNativeOwnedClientStreamsAndCancellationReapsBeforeReturn(t *testing.T) {
	program, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	program, e = filepath.EvalSymlinks(program)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out := &notifyWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	root := bridgeRoot(t)
	go func() {
		done <- nativeProcess(ctx, program, []string{"-test.run=^TestNativeProcessHelper$"}, root, []string{"WORKHARBOR_NATIVE_PROCESS_HELPER=wait"}, strings.NewReader(""), out)
	}()
	select {
	case e := <-done:
		t.Fatal("owned client did not stream", e)
	case <-out.ready:
	case <-ctx.Done():
		t.Fatal("stream blocked")
	}
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled child reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("owned child not killed/reaped")
	}
	if out.buffer.String() != "ready\n" {
		t.Fatal("stdout altered")
	}
}
func TestNativeOwnedClientRequiresDeadlineAndAbsoluteProgram(t *testing.T) {
	for _, ctx := range []context.Context{context.Background(), func() context.Context { c, cancel := context.WithCancel(context.Background()); cancel(); return c }()} {
		if e := nativeProcess(ctx, "relative", nil, "/tmp", nil, nil, os.Stdout); e == nil {
			t.Fatal("unbounded/relative execution")
		}
	}
}
