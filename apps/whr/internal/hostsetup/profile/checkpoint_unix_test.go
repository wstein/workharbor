//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package profile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointLossPoisonsProfileAndReleasesLock(t *testing.T) {
	home := t.TempDir()
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	start := Start{RunID: "0123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}
	run, err := store.Begin(start, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	run.text.Close() // only this test's exclusively owned descriptor
	if err = run.Checkpoint(); !errors.Is(err, ErrProtocol) {
		t.Fatal("lost text handle accepted", err)
	}
	if _, err = run.StoreProfile(HostProfile{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Updated: now.Format(time.RFC3339), Items: map[string]string{}}); err == nil {
		t.Fatal("poisoned checkpoint permitted profile write")
	}
	if _, err = os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("poisoned journal mutated profile")
	}
	run.Close()
	if err = run.Checkpoint(); !errors.Is(err, ErrProtocol) {
		t.Fatal("closed run accepted checkpoint")
	}
	start.RunID = "1123456789abcdef"
	next, err := store.Begin(start, now)
	if err != nil {
		t.Fatal("checkpoint failure retained lock", err)
	}
	next.Close()
}
