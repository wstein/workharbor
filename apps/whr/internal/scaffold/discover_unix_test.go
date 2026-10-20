//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"golang.org/x/sys/unix"
)

func TestInitDiscoverySpecialFileRefused(t *testing.T) {
	for _, raced := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing FIFO", true: "FIFO after inspection"}[raced], func(t *testing.T) {
			dir := fixture(t)
			name := ".devcontainer/devcontainer.json"
			target := filepath.Join(dir, name)
			replace := func() {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if !raced {
				replace()
			}
			_, err := discover(context.Background(), dir, func(path string) {
				if raced && path == name {
					replace()
				}
			})
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("special target must be refused without reading: %v", err)
			}
		})
	}
}
