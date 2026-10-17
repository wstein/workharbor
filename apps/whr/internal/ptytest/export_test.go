// SPDX-License-Identifier: EUPL-1.2

package ptytest

// SetKiller replaces the process-group killer for one test and returns the
// function that restores it.
func SetKiller(f func(pid int)) (restore func()) {
	old := killer
	killer = f
	return func() { killer = old }
}

// Work reports how many bytes the session projected, drew and searched so far.
// It grows linearly with the output; a quadratic matcher would not.
func (s *Session) Work() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.work
}

// Pending reports how many visible bytes Expect has examined without a match.
func (s *Session) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.plain)
}
