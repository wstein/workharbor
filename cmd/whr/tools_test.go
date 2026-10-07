package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

// toolsFixture serves one pinned tool and returns a store dir and a pins file.
func toolsFixture(t *testing.T) (store, pins string) {
	t.Helper()
	bin := []byte("#!/bin/sh\necho claude\n")
	sum := sha256.Sum256(bin)
	hash := hex.EncodeToString(sum[:])
	vendor := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rel/9.9.9/manifest.json":
			fmt.Fprintf(w, `{"platforms":{"linux-arm64":{"checksum":%q}}}`, hash)
		case "/rel/9.9.9/linux-arm64/claude":
			_, _ = w.Write(bin)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(vendor.Close)
	toolsClient = vendor.Client()
	t.Cleanup(func() { toolsClient = nil })
	dir := t.TempDir()
	pins = filepath.Join(dir, "pins.json")
	body := fmt.Sprintf(`{"tools":[{"name":"claude","version":"9.9.9","platform":"linux-arm64","base_url":%q,"sha256":%q}]}`, vendor.URL+"/rel", hash)
	if err := os.WriteFile(pins, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store = filepath.Join(dir, "tools")
	t.Cleanup(func() { // the store is read-only
		_ = filepath.WalkDir(store, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o750) //nolint:gosec // a test cleaning up
			}
			return nil
		})
	})
	return store, pins
}

// A bad name in -tools exits with the usage code, says why on stderr and
// prints nothing on stdout; names are case-sensitive and a repeat is harmless.
func TestToolsBuildValidatesToolNames(t *testing.T) {
	for _, tc := range []struct {
		list, msg string
		code      int
	}{
		{"claude,bogus", `"bogus"`, exitcode.Usage},
		{"Claude", `"Claude"`, exitcode.Usage},
		{"", "empty tool name", exitcode.Usage},
		{"claude,", "empty tool name", exitcode.Usage},
		{"claude, claude", "", exitcode.OK},
	} {
		t.Run(tc.list, func(t *testing.T) {
			store, pins := toolsFixture(t)
			var out, errOut bytes.Buffer
			args := []string{"tools", "build", "-store", store, "-pins", pins, "-tools", tc.list}
			if code := run(args, &out, &errOut); code != tc.code {
				t.Fatalf("exit %d, want %d; stderr %q", code, tc.code, errOut.String())
			}
			if tc.code != exitcode.OK && (out.Len() != 0 || !strings.Contains(errOut.String(), tc.msg)) {
				t.Errorf("stdout %q stderr %q", out.String(), errOut.String())
			}
		})
	}
}

// runTools derives from the root context: a cancelled root stops the build.
func TestToolsBuildUsesRootContext(t *testing.T) {
	store, pins := toolsFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	args := []string{"tools", "build", "-store", store, "-pins", pins}
	if code := execute(ctx, args, strings.NewReader(""), &out, &errOut); code != exitcode.Error {
		t.Fatalf("exit %d, want Error; stdout %q stderr %q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "canceled") {
		t.Errorf("stderr %q does not report the cancellation", errOut.String())
	}
}
