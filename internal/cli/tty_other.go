//go:build !unix

package cli

// Resizes never sends: this platform has no window-change signal.
func (osTTY) Resizes() (<-chan struct{}, func()) { return make(chan struct{}), func() {} }
