package web

import (
	"errors"
	"net/http"
	"sync"

	"github.com/wstein/workharbor/internal/store"
)

var errNotYet = errors.New("web: nothing stored under this key")

// onceKeys serialises the requests of one idempotency key in this process.
type onceKeys struct {
	mu    sync.Mutex
	locks map[string]*keyLock
}

type keyLock struct {
	mu   sync.Mutex
	refs int
}

func (o *onceKeys) lock(key string) func() {
	o.mu.Lock()
	if o.locks == nil {
		o.locks = map[string]*keyLock{}
	}
	l := o.locks[key]
	if l == nil {
		l = &keyLock{}
		o.locks[key] = l
	}
	l.refs++
	o.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		o.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(o.locks, key)
		}
		o.mu.Unlock()
	}
}

// once runs a write at most once per idempotency key (design §9.2, issue #30):
// the first request runs and keeps the place it redirects to, and a repeat of
// the same form (a double tap, a retry) gets the same redirect without running
// again. A write that failed keeps nothing, so a retry runs it again. A request
// without a key is refused: every form of the UI carries one.
func (s *Server) once(r *http.Request, run func() (location string, err error)) (string, error) {
	key := r.PostForm.Get("key")
	if key == "" || len(key) > 100 {
		return "", errBadForm
	}
	key = "web:" + key
	unlock := s.keys.lock(key)
	defer unlock()
	hash := store.RequestHash(r.Method, r.URL.Path, r.PostForm.Encode())
	stored, replayed, err := s.opt.Store.Do(r.Context(), key, hash, func(*store.Tx) ([]byte, error) { return nil, errNotYet })
	switch {
	case err == nil && replayed:
		return string(stored), nil
	case err != nil && !errors.Is(err, errNotYet):
		return "", err
	}
	loc, err := run()
	if err != nil {
		return "", err
	}
	if _, _, err := s.opt.Store.Do(r.Context(), key, hash, func(*store.Tx) ([]byte, error) { return []byte(loc), nil }); err != nil {
		return "", err
	}
	return loc, nil
}
