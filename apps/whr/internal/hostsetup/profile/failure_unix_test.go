//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendFailurePoisonsManagedMutation(t *testing.T) {
	home := t.TempDir()
	s, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	start := Start{RunID: "0123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}
	r, err := s.Begin(start, now)
	if err != nil {
		t.Fatal(err)
	}
	// Close only this run's exclusively owned descriptor to force a durable-write
	// failure independently of host privileges (chmod is ineffective under root).
	r.json.Close()
	if err = r.Step(StepAudit{ID: "jq.detect", Item: "jq", Kind: "detect", Result: "unchanged"}, now); err == nil {
		t.Fatal("closed journal write passed")
	}
	p := HostProfile{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Updated: now.Format(time.RFC3339), Items: map[string]string{}}
	if _, err = r.StoreProfile(p); err == nil {
		t.Fatal("failed journal permitted mutation")
	}
	if _, err = os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("profile changed")
	}
	r.Close()
	start.RunID = "1123456789abcdef"
	next, err := s.Begin(start, now)
	if err != nil {
		t.Fatal("failed run retained lock", err)
	}
	next.Close()
}
func TestProfileChecksJournalBeforeMutation(t *testing.T) {
	home := t.TempDir()
	s, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	r, err := s.Begin(Start{RunID: "0123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, now)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.text.Close()
	p := HostProfile{SchemaVersion: 1, CatalogueVersion: "2026.10.1", Updated: now.Format(time.RFC3339), Items: map[string]string{}}
	if _, err = r.StoreProfile(p); err == nil {
		t.Fatal("unwritable text journal permitted profile mutation")
	}
}
