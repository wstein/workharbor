//go:build !darwin

package setup

import (
	"os"
	"testing"
)

func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	t.Skip("pseudo terminal helper is for macOS only")
	return nil, nil
}
