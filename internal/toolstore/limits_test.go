package toolstore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadsAreHTTPSOnly(t *testing.T) {
	bin := []byte("tool")
	v := newVendor(t, bin)
	strict := &Store{Root: t.TempDir()}
	if _, err := strict.Download(bg, v.pin(sum(bin))); !errors.Is(err, ErrInsecure) {
		t.Errorf("a plain http base = %v, want ErrInsecure", err)
	}
	if v.manifest != 0 || v.hits != 0 {
		t.Error("the vendor was contacted over plain http")
	}

	// An https host that redirects to plain http is refused too.
	plain := v.srv.URL
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain+r.URL.Path, http.StatusFound) //nolint:gosec // the test redirects on purpose
	}))
	defer tls.Close()
	s := &Store{Root: t.TempDir(), Client: tls.Client()}
	p := v.pin(sum(bin))
	p.BaseURL = tls.URL + "/rel"
	if _, err := s.Download(bg, p); !errors.Is(err, ErrInsecure) {
		t.Errorf("a redirect to http = %v, want ErrInsecure", err)
	}
	if v.manifest != 0 || v.hits != 0 {
		t.Error("the redirect to plain http was followed")
	}
	assertEmpty(t, s)
}

func TestDownloadsAreCapped(t *testing.T) {
	bin := []byte(strings.Repeat("x", 4096))
	v := newVendor(t, bin)
	s := newStore(t)
	s.MaxBytes = 1024
	if _, err := s.Download(bg, v.pin(sum(bin))); !errors.Is(err, ErrTooLarge) {
		t.Errorf("an oversized download = %v, want ErrTooLarge", err)
	}
	assertEmpty(t, s)
	s.MaxBytes = 4096
	if _, err := s.Download(bg, v.pin(sum(bin))); err != nil {
		t.Errorf("a download at the limit: %v", err)
	}
}

// A body without a Content-Length is cut at the limit as it streams.
func TestAStreamedDownloadIsCapped(t *testing.T) {
	bin := []byte(strings.Repeat("y", 8192))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(`{"platforms":{"linux-arm64":{"checksum":"` + sum(bin) + `"}}}`))
			return
		}
		for i := 0; i < len(bin); i += 1024 {
			_, _ = w.Write(bin[i : i+1024])
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()
	s := newStore(t)
	s.MaxBytes = 2048
	p := Pin{Name: "tool", Version: "1.2.3", Platform: "linux-arm64", BaseURL: srv.URL + "/rel", SHA256: sum(bin)}
	if _, err := s.Download(bg, p); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a streamed oversized download = %v, want ErrTooLarge", err)
	}
	assertEmpty(t, s)
}
