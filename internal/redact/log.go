package redact

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// maxLine bounds how much a Writer holds while waiting for a newline.
var maxLine = 1 << 20

// Writer redacts what is written through it, a line at a time, so a secret
// split across two Write calls is still found. Call Flush when the stream
// ends to write what is left.
type Writer struct {
	w io.Writer
	r *Redactor

	mu  sync.Mutex
	buf []byte
}

// NewWriter returns a Writer that redacts with r before writing to w.
func NewWriter(w io.Writer, r *Redactor) *Writer { return &Writer{w: w, r: r} }

// Write implements io.Writer. It reports all of p as written once p is
// accepted, whether or not a line is complete yet.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i+1]
		if _, err := w.w.Write(w.r.Bytes(line)); err != nil {
			return 0, err
		}
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxLine {
		if _, err := w.w.Write(w.r.Bytes(w.buf)); err != nil {
			return 0, err
		}
		w.buf = nil
	}
	return len(p), nil
}

// Flush writes the incomplete last line, redacted.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return nil
	}
	_, err := w.w.Write(w.r.Bytes(w.buf))
	w.buf = nil
	return err
}

type handler struct {
	inner slog.Handler
	r     *Redactor
}

// NewHandler wraps a slog handler so that messages and attributes are
// redacted before the inner handler sees them. An attribute of a type it
// cannot look into is written as its redacted %+v text.
func NewHandler(inner slog.Handler, r *Redactor) slog.Handler { return &handler{inner: inner, r: r} }

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.String(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.attr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.attr(a)
	}
	return &handler{inner: h.inner.WithAttrs(redacted), r: h.r}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name), r: h.r}
}

func (h *handler) attr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.r.String(v.String()))
	case slog.KindGroup:
		group := v.Group()
		redacted := make([]any, len(group))
		for i, g := range group {
			redacted[i] = h.attr(g)
		}
		return slog.Group(a.Key, redacted...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, h.r.String(err.Error()))
		}
		return slog.String(a.Key, h.r.String(fmt.Sprintf("%+v", v.Any())))
	default:
		return slog.Attr{Key: a.Key, Value: v}
	}
}
