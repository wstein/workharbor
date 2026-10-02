package web

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// events streams a task's events as HTML fragments over SSE for the live
// transcript (design §9.3). Durable events carry their sequence number as the
// SSE id, so a browser that reconnects with Last-Event-ID, or the page with
// ?since=, gets the missed ones replayed before the live ones; ephemeral events
// have no id and are never stored. The session was checked before this runs.
func (s *Server) events(w http.ResponseWriter, r *http.Request, sess Session) {
	task := domain.ID(r.PathValue("task"))
	since, err := sinceOf(r)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, r, sess, fmt.Errorf("streaming is not supported by this connection"))
		return
	}
	if _, err := s.be.Show(r.Context(), task); err != nil {
		s.fail(w, r, sess, err)
		return
	}
	// The stream ends with its session: sign-out, a revoke from another device and
	// expiry all cancel it, and a heartbeat checks again without counting as use.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	streams, _ := s.opt.Auth.(Streams)
	if streams != nil {
		defer streams.Watch(r, cancel)()
	}
	ch, err := s.be.Subscribe(ctx, task, since)
	if err != nil {
		s.fail(w, r, sess, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	tick := time.NewTicker(s.opt.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if streams != nil && !streams.Peek(r) {
				return
			}
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return // dropped: the browser reconnects with Last-Event-ID
			}
			if err := writeFragment(w, r, e); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func sinceOf(r *http.Request) (int64, error) {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("since")
	}
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, &httpError{status: http.StatusBadRequest, msg: "the last event ID must be a non-negative integer"}
	}
	return n, nil
}

// writeFragment sends one event as the row the page shows. The row is rendered
// by the template, which escapes its text; an SSE data field cannot hold a
// newline, so each line of the fragment is its own data line, which the browser
// joins again.
func writeFragment(w http.ResponseWriter, r *http.Request, e domain.Event) error {
	var buf bytes.Buffer
	if err := eventItem(eventRowOf(e)).Render(r.Context(), &buf); err != nil {
		return err
	}
	if e.Seq != 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", e.Seq); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "event: ev\n"); err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", strings.TrimRight(line, "\r")); err != nil {
			return err
		}
	}
	_, err := fmt.Fprint(w, "\n")
	return err
}
