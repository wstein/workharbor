//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package profile

// Checkpoint verifies both owned durable handles before an action starts. It
// records no invented outcome and exposes no journal handle or lock lifecycle.
func (r *Run) Checkpoint() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.finished || r.poisoned || r.json == nil || r.text == nil {
		return ErrProtocol
	}
	if r.json.Sync() != nil || r.text.Sync() != nil {
		r.poisoned = true
		return ErrProtocol
	}
	return nil
}
