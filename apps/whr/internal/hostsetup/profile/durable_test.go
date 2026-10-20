// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package profile_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

func TestRunLockExclusive(t *testing.T) {
	home := t.TempDir()
	s, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Begin(profile.Start{RunID: "0123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s2, err := profile.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err = s2.Begin(profile.Start{RunID: "1123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, time.Now()); err == nil {
		t.Fatal("second run acquired lock")
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r2, err := s2.Begin(profile.Start{RunID: "1123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r2.Close()
	info, err := os.Stat(filepath.Join(home, ".local/state/whr/protocols/0123456789abcdef.jsonl"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("protocol not durable/private", err)
	}
}
