// SPDX-License-Identifier: EUPL-1.2

package ptytest

import (
	"strings"
	"testing"
	"time"
)

// An unbuffered notification makes each producer step wait for the expect
// loop. A scheduler may combine two adjacent chunks, but cannot coalesce the
// entire flood into one search. No PTY read sizes or wall-clock ratios enter
// this oracle: an old-prefix rescan costs quadratically more than its bound.
func TestIncrementalMatcherWorkIsLinear(t *testing.T) {
	const chunks, chunkSize = 64, 1024
	s := &Session{t: t, opts: Options{Timeout: 10 * time.Second},
		changed: make(chan struct{}), drained: make(chan struct{}), scr: newScreen(80, 24)}
	stop, done := make(chan struct{}), make(chan struct{})
	defer func() { close(stop); <-done }()
	go func() {
		defer close(done)
		for i := 0; i <= chunks; i++ {
			text := strings.Repeat("x", chunkSize)
			if i == chunks {
				text = "END"
			}
			s.mu.Lock()
			s.buf = append(s.buf, text...)
			s.mu.Unlock()
			select {
			case s.changed <- struct{}{}:
			case <-stop:
				return
			}
		}
	}()
	s.Expect("END")
	// Project and draw each byte once, search each new byte once, and allow
	// a two-byte overlap for every controlled wake-up (plus the initial scan).
	if work, limit := s.Work(), uint64(3*(chunks*chunkSize+3)+2*(chunks+2)); work > limit {
		t.Fatalf("incremental matcher work %d exceeds linear bound %d", work, limit)
	}
}

// Feed a fixed number of chunks directly through both real consumers. This
// keeps the unterminated-tail and discard-state work oracle deterministic
// while the companion external tests retain actual PTY coverage.
func TestIncrementalSequenceWorkIsLinear(t *testing.T) {
	for _, seq := range []string{"\x1b]0;", "\x1bP", "\x1b_", "\x1b[1;", "\x1b[?"} {
		for _, terminated := range []bool{false, true} {
			s := &Session{scr: newScreen(80, 24)}
			s.buf = append(s.buf, seq...)
			s.advanceLocked()
			filler := strings.Repeat("x", 1024)
			if strings.HasPrefix(seq, "\x1b[") {
				filler = strings.Repeat("1", 1024)
			}
			for range 64 {
				s.buf = append(s.buf, filler...)
				s.advanceLocked()
			}
			if terminated {
				s.buf = append(s.buf, "\x18END"...)
				s.advanceLocked()
			}
			want := ""
			if terminated {
				want = "END"
			}
			if string(s.plain) != want || s.scr.text() != want {
				t.Fatalf("%q terminated=%t: visible text %.40q, screen %.40q, want %q", seq, terminated, s.plain, s.scr.text(), want)
			}
			// Each consumer visits every byte once after entering discard
			// mode; the initially retained escape tail costs less than one
			// additional 64 KiB flood across these fixed 1 KiB chunks.
			if work, limit := s.work, uint64(3*len(s.buf)); work > limit {
				t.Fatalf("%q terminated=%t: sequence work %d exceeds linear bound %d", seq, terminated, work, limit)
			}
		}
	}
}
