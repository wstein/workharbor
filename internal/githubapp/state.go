package githubapp

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

const stateBytes = 32

// States holds the `state` values the create command minted. A value is valid
// for a few minutes and is used once: the redirect that carries it back from
// GitHub spends it, so a replay finds nothing. Values are compared in constant
// time against every live one, never looked up by key.
type States struct {
	ttl time.Duration
	now func() time.Time

	mu   sync.Mutex
	live []stateRec
}

type stateRec struct {
	value string
	org   string
	exp   time.Time
}

// NewStates returns an empty set. A zero ttl means ten minutes; a nil clock
// means time.Now.
func NewStates(ttl time.Duration, now func() time.Time) *States {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &States{ttl: ttl, now: now}
}

// Mint returns a new unguessable value that is bound to org ("" for the
// operator's own account) and when it expires.
func (s *States) Mint(org string) (string, time.Time) {
	b := make([]byte, stateBytes)
	if _, err := rand.Read(b); err != nil {
		panic("githubapp: no randomness: " + err.Error())
	}
	rec := stateRec{value: hex.EncodeToString(b), org: org, exp: s.now().Add(s.ttl)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.live = append(s.live, rec)
	return rec.value, rec.exp
}

// Peek reports whether value is live, without spending it, and what it is bound
// to. The start page uses it.
func (s *States) Peek(value string) (org string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findLocked(value)
	if i < 0 {
		return "", false
	}
	return s.live[i].org, true
}

// Take spends value: it is live once, and after Take it is gone.
func (s *States) Take(value string) (org string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findLocked(value)
	if i < 0 {
		return "", false
	}
	org = s.live[i].org
	s.live = append(s.live[:i], s.live[i+1:]...)
	return org, true
}

// findLocked compares value with every live one in constant time per entry and
// returns the index of the match, or -1. An expired value is dropped first.
func (s *States) findLocked(value string) int {
	s.sweepLocked()
	idx := -1
	for i, rec := range s.live {
		if len(value) == len(rec.value) && subtle.ConstantTimeCompare([]byte(value), []byte(rec.value)) == 1 {
			idx = i
		}
	}
	return idx
}

func (s *States) sweepLocked() {
	now := s.now()
	kept := s.live[:0]
	for _, rec := range s.live {
		if now.Before(rec.exp) {
			kept = append(kept, rec)
		}
	}
	s.live = kept
}
