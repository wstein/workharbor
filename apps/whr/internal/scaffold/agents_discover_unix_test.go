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

func TestInitAgentsFIFOAndReplacedFIFORefused(t *testing.T) {
	for _, raced := range []bool{false, true} {
		t.Run(map[bool]string{false: "FIFO", true: "replaced FIFO"}[raced], func(t *testing.T) {
			dir := fixture(t)
			target := filepath.Join(dir, "AGENTS.md")
			replace := func() {
				_ = os.Remove(target)
				if err := unix.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if raced {
				if err := os.WriteFile(target, []byte("owner\n"), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				replace()
			}
			_, err := discover(context.Background(), dir, func(name string) {
				if raced && name == "AGENTS.md" {
					replace()
				}
			})
			if exitcode.FromError(err) != exitcode.Conflict {
				t.Fatalf("FIFO not refused: %v", err)
			}
			// Opt-out must not open a FIFO at all.
			if _, err := DiscoverWithOptions(context.Background(), dir, Options{NoAgentsBlock: true, DryRun: true}); err != nil {
				t.Fatalf("opt-out touched FIFO: %v", err)
			}
		})
	}
}
