package redact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const canary = "canary-0123456789abcdef-secret"

func newCanaryRedactor() *Redactor {
	r := New()
	r.Add(canary)
	return r
}

func TestWriterRedactsALineAcrossWrites(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, newCanaryRedactor())
	// The secret is split across three writes and the line ends in the last one.
	for _, part := range []string{"token is canary-0123", "456789abcd", "ef-secret now\nnext line\n"} {
		if n, err := w.Write([]byte(part)); err != nil || n != len(part) {
			t.Fatalf("Write(%q) = %d, %v", part, n, err)
		}
	}
	got := out.String()
	if strings.Contains(got, canary) || strings.Contains(got, "0123456789abcdef") {
		t.Errorf("the secret leaked: %q", got)
	}
	if got != "token is "+Mask+" now\nnext line\n" {
		t.Errorf("output = %q", got)
	}
}

func TestWriterHoldsAnIncompleteLineUntilFlush(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, newCanaryRedactor())
	_, _ = w.Write([]byte("partial " + canary))
	if out.Len() != 0 {
		t.Fatalf("an incomplete line was written early: %q", out.String())
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "partial "+Mask {
		t.Errorf("flushed = %q", out.String())
	}
	if err := w.Flush(); err != nil || out.Len() != len("partial "+Mask) {
		t.Errorf("a second Flush must write nothing: %v", err)
	}
}

func TestWriterBoundsWhatItHolds(t *testing.T) {
	saved := maxLine
	maxLine = 1024
	t.Cleanup(func() { maxLine = saved })

	var out bytes.Buffer
	w := NewWriter(&out, newCanaryRedactor())
	chunk := strings.Repeat("x", 600) // no newline anywhere
	for range 3 {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if out.Len() == 0 {
		t.Error("a line without a newline must not be held forever")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriterReportsAnUnderlyingError(t *testing.T) {
	w := NewWriter(failingWriter{}, newCanaryRedactor())
	if _, err := w.Write([]byte("a line\n")); err == nil {
		t.Error("an error of the underlying writer must be returned")
	}
}

type secretHolder struct {
	Name  string
	Token string
}

func TestSlogHandlerRedactsMessagesAndEveryKindOfAttribute(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), newCanaryRedactor()))
	log = log.With("preset", "with "+canary).WithGroup("outer")

	log.Info("message with "+canary,
		"plain", "nothing secret",
		"count", 42,
		"secret", canary,
		slog.Group("inner", slog.String("deep", "x "+canary+" y"), slog.Int("n", 7)),
		"err", fmt.Errorf("request failed: %w", errors.New("bad token "+canary)),
		"struct", secretHolder{Name: "bot", Token: canary},
		"gh", "clone with "+ghToken,
	)
	got := buf.String()
	if strings.Contains(got, canary) || strings.Contains(got, ghToken) {
		t.Fatalf("a secret reached the log: %s", got)
	}
	for _, keep := range []string{"nothing secret", `"count":42`, `"n":7`, "request failed", "Name:bot", "message with", "clone with"} {
		if !strings.Contains(got, keep) && !strings.Contains(got, strings.ReplaceAll(keep, `"`, `\"`)) {
			t.Errorf("context %q was lost: %s", keep, got)
		}
	}
}

func TestSlogHandlerKeepsLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}), newCanaryRedactor())
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("the wrapper must keep the inner handler's level")
	}
	slog.New(h).Info("hidden " + canary)
	if buf.Len() != 0 {
		t.Errorf("a record below the level was written: %q", buf.String())
	}
	slog.New(h).Warn("shown " + canary)
	if got := buf.String(); !strings.Contains(got, "shown "+Mask) || strings.Contains(got, canary) {
		t.Errorf("warn record = %q", got)
	}
}
