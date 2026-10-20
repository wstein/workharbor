//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
//
package profile_test

import (
	"golang.org/x/sys/unix"
	"testing"
)

func makeFIFO(t *testing.T, p string) {
	t.Helper()
	if err := unix.Mkfifo(p, 0600); err != nil {
		t.Fatal(err)
	}
}
