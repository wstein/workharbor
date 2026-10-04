package hostgit

import (
	"context"
	"errors"
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
