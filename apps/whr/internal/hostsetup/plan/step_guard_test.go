//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
)

func TestStepGuardOwnsDependencySnapshotAndJournalLifetime(t *testing.T) {
	store, err := profile.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.Begin(profile.Start{RunID: "0123456789abcdef", Command: "setup", Platform: "darwin-arm64", CatalogueVersion: "2026.10.1", Answer: "yes"}, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	completed := map[string]bool{"jq.dotfile_create": true, "bat.brew_install": true}
	guard := guardFor(run, Step{DependsOn: []string{"jq.dotfile_create"}}, completed)
	completed["jq.dotfile_create"] = false
	if !guard.Completed("jq.dotfile_create") || guard.Completed("bat.brew_install") || guard.Completed("unknown") {
		t.Fatal("dependency evidence was not the exact owned snapshot")
	}
	if err = guard.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	run.Close()
	if err = guard.Checkpoint(); err == nil {
		t.Fatal("closed journal accepted checkpoint")
	}
}
