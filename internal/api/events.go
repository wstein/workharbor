package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// events streams a task's events as server-sent events (design §9.7). Durable
// events carry their sequence number as the SSE id, so a client reconnects with
// Last-Event-ID (or ?since=) and gets the missed ones replayed from the store
// before the live ones. Ephemeral events (token deltas) and the heartbeat
// comments come from memory, have no id and are never stored.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	task, err := idParam(r, "task")
	if err != nil {
		writeError(w, err)
		return
	}
	since, err := sinceOf(r)
	if err != nil {
		writeError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, fmt.Errorf("streaming is not supported by this connection"))
		return
	}
	if _, err := s.be.Show(r.Context(), task); err != nil { // an unknown task is a 404, not an empty stream
		s.fail(w, err)
		return
	}
	ch, err := s.be.Subscribe(r.Context(), task, since)
	if err != nil {
		s.fail(w, err)
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
		case <-r.Context().Done():
			return
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return // the subscriber was dropped or the server stops: the client reconnects
			}
			if err := writeEvent(w, e); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// sinceOf reads the position to resume from: Last-Event-ID wins over ?since=.
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
		return 0, usageError{"the last event ID must be a non-negative integer"}
	}
	return n, nil
}

func writeEvent(w http.ResponseWriter, e domain.Event) error {
	data, err := json.Marshal(eventOf(e))
	if err != nil {
		return err
	}
	if e.Seq != 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", e.Seq); err != nil {
			return err
		}
	}
	// The kind is one of our own constants; a newline in it would end the field.
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sanitizeField(string(e.Kind)), data)
	return err
}

func sanitizeField(s string) string {
	out := make([]rune, 0, len(s))
	for _, c := range s {
		if c == '\n' || c == '\r' {
			c = ' '
		}
		out = append(out, c)
	}
	return string(out)
}
