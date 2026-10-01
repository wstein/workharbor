package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/wstein/workharbor/internal/store"
)

// errNotYet is returned from a lookup-only Do: nothing is stored under the key.
var errNotYet = errors.New("api: no response stored under this key")

type storedResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// lockKey serialises the requests of one key in this process, so two requests
// with one key cannot both run. The returned function unlocks.
func (s *Server) lockKey(key string) func() {
	s.mu.Lock()
	l := s.locks[key]
	if l == nil {
		l = &keyLock{}
		s.locks[key] = l
	}
	l.refs++
	s.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		s.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(s.locks, key)
		}
		s.mu.Unlock()
	}
}

// idempotent runs a mutating request (design §9.7). Without an
// Idempotency-Key it just runs. With one, the first request runs and its
// successful response is stored under the key and a hash of the request; the
// same key and request returns that response without running again; the same
// key with another request is a conflict. A request that failed stores nothing,
// so a retry runs again. The effect (a runtime, an agent) cannot be part of the
// database transaction, so the response is stored after it: the domain rules
// make a rerun after a crash in between a conflict, not a duplicate.
func (s *Server) idempotent(w http.ResponseWriter, r *http.Request, body []byte, run func() (int, any, error)) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		status, data, err := run()
		if err != nil {
			s.fail(w, err)
			return
		}
		writeOK(w, status, data)
		return
	}
	unlock := s.lockKey(key)
	defer unlock()
	hash := store.RequestHash(r.Method, r.URL.Path, string(body))

	// Is there a stored response? A lookup is a Do whose work finds nothing to do.
	stored, replayed, err := s.opt.Store.Do(r.Context(), key, hash, func(*store.Tx) ([]byte, error) { return nil, errNotYet })
	switch {
	case err == nil && replayed:
		s.replay(w, stored)
		return
	case err != nil && !errors.Is(err, errNotYet):
		s.fail(w, err)
		return
	}

	status, data, err := run()
	if err != nil {
		s.fail(w, err)
		return
	}
	raw, err := json.Marshal(Envelope{SchemaVersion: SchemaVersion, OK: true, Data: data})
	if err != nil {
		s.fail(w, err)
		return
	}
	record, _ := json.Marshal(storedResponse{Status: status, Body: raw})
	out, _, err := s.opt.Store.Do(r.Context(), key, hash, func(*store.Tx) ([]byte, error) { return record, nil })
	if err != nil {
		// The request ran but its response could not be kept. Say so: a retry
		// with the same key will run again and meet the domain's own rules.
		s.internal(err)
		writeOK(w, status, data)
		return
	}
	var sr storedResponse
	if json.Unmarshal(out, &sr) != nil {
		writeOK(w, status, data)
		return
	}
	writeStored(w, sr, false)
}

func (s *Server) replay(w http.ResponseWriter, stored []byte) {
	var sr storedResponse
	if err := json.Unmarshal(stored, &sr); err != nil {
		s.internal(err)
		writeError(w, errors.New("the stored response could not be read"))
		return
	}
	writeStored(w, sr, true)
}

func writeStored(w http.ResponseWriter, sr storedResponse, replayed bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if replayed {
		w.Header().Set("Idempotent-Replay", "true")
	}
	w.WriteHeader(sr.Status)
	_, _ = w.Write(append([]byte(sr.Body), '\n'))
}
