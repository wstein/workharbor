package hostgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTransportFault(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want bool
	}{
		{"fatal: unable to access 'https://x/': Could not resolve host: github.com", true},
		{"fatal: unable to access: Failed to connect: Connection refused", true},
		{"error: RPC failed; HTTP 502 curl 22 The requested URL returned error: 502", true},
		{"fatal: the remote end hung up unexpectedly", true},
		// a refusal at the POST ends with the same hang-up line: not retried for ever
		{"error: RPC failed; HTTP 413 curl 22 The requested URL returned error: 413\nfatal: the remote end hung up unexpectedly", false},
		{"error: RPC failed; HTTP 403 curl 22 The requested URL returned error: 403\nfatal: the remote end hung up unexpectedly", false},
		{"error: RPC failed; HTTP 422 curl 22 The requested URL returned error: 422\nfatal: early EOF", false},
		{"error: RPC failed; HTTP 429 curl 22 The requested URL returned error: 429\nfatal: the remote end hung up unexpectedly", true},
		{"error: RPC failed; HTTP 403 curl 22 returned error: 403: You have exceeded a secondary rate limit", true},
		{"error: RPC failed; curl 56 OpenSSL SSL_read: Connection reset by peer\nfatal: early EOF", true},
		{"fatal: early EOF", true},
		{"remote: Permission to x/y.git denied to the bot.\nfatal: unable to access: The requested URL returned error: 403", false},
		// the server's own lines never count: a refusal can word them as it likes
		{"remote: error: GH013: rate limit hook says connection reset\nfatal: unable to access: The requested URL returned error: 403", false},
		{"remote: Rate limit exceeded by our pre-receive hook\n ! [remote rejected] x", false},
		{"  remote: could not resolve host\nerror: failed to push some refs", false},
		{"remote: internal\nfatal: unable to access: Could not resolve host: github.com", true},
		{"fatal: repository 'https://x/y.git/' not found", false},
		{"! [remote rejected] abc -> agent/x (protected branch hook declined)", false},
	} {
		if got := transportFault(context.Background(), errors.New(tc.msg)); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.msg, got, tc.want)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if !transportFault(ctx, errors.New("signal: killed")) {
		t.Error("a push that ran out of time is a transport fault")
	}
}

// The error of a failed push is built by run and runToken as
// "git <args>: <cause>: <stderr>", which glues git's first stderr line to the
// prefix. The judgement must see the stderr alone, so a first "remote:" line is
// still the server's text.
func TestTransportFaultJudgesStderrOfTheRealError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stderr string
		want   bool
	}{
		{"a server line first", "remote: error: GH013: rate limit hook says connection reset\nfatal: unable to access: The requested URL returned error: 403", false},
		{"a transport fault after a server line", "remote: internal\nfatal: unable to access: Could not resolve host: github.com", true},
		{"git's own line first", "fatal: unable to access: Could not resolve host: github.com", true},
	} {
		for _, viaToken := range []bool{false, true} {
			err := failingGit(t, tc.stderr, viaToken)
			if got := transportFault(context.Background(), err); got != tc.want {
				t.Errorf("%s (token %v): got %v, want %v; error %q", tc.name, viaToken, got, tc.want, err)
			}
		}
	}
}

// failingGit returns the error of a real run or runToken whose git (a script)
// exits 128 with the given standard error.
func failingGit(t *testing.T, stderr string, viaToken bool) error {
	t.Helper()
	dir := t.TempDir()
	msg := filepath.Join(dir, "stderr")
	if err := os.WriteFile(msg, []byte(stderr+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat '"+msg+"' >&2\nexit 128\n"), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
	g, err := New()
	if err != nil {
		t.Fatal(err)
	}
	g.bin = script
	if viaToken {
		_, err = g.runToken(context.Background(), dir, nil, "", "push", "x")
	} else {
		_, err = g.run(context.Background(), dir, true, nil, "push", "x")
	}
	if err == nil {
		t.Fatal("the script did not fail")
	}
	return err
}
