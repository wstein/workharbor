// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStageDurableIntentAndMonotonicRecovery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	s, err := openStageStore(root, "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	op := strings.Repeat("1", 32)
	if err := s.Begin(op, "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("allocating", "create_volume", "attempted"); err != nil {
		t.Fatal(err)
	}
	records, err := s.Records()
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%v err=%v", records, err)
	}
	if records[1].Sequence != 2 || records[1].PreviousHash != records[0].Hash || records[1].State != "allocating" {
		t.Fatalf("bad chain: %+v", records)
	}
	if err := s.Begin(strings.Repeat("2", 32), records[0].Fingerprint); err == nil || !strings.Contains(err.Error(), "retained attempt") {
		t.Fatalf("retained attempt admitted: %v", err)
	}
	if err := s.Advance("complete", "remove_home", "observed"); err == nil || !strings.Contains(err.Error(), "state transition") {
		t.Fatalf("skipped state: %v", err)
	}
	path := filepath.Join(s.path, "journal.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	rows[0]["fingerprint"] = "sha256:" + strings.Repeat("b", 64)
	b, _ = json.Marshal(rows)
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Records(); err == nil || !strings.Contains(err.Error(), "journal integrity") {
		t.Fatalf("corruption admitted: %v", err)
	}
}

func TestLocalStageStateModesAndNoFollow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	s, err := openStageStore(root, "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Begin(strings.Repeat("1", 32), "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{root, s.path} {
		st, e := os.Stat(p)
		if e != nil || st.Mode().Perm() != 0700 {
			t.Fatalf("private directory %q: %v %v", p, st, e)
		}
	}
	st, err := os.Stat(filepath.Join(s.path, "journal.json"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("private journal: %v %v", st, err)
	}
	if _, err = openStageStore(root, "daemon", "unix:///tmp/docker.sock"); err == nil || !strings.Contains(err.Error(), "operation lock") {
		t.Fatalf("concurrent lock admitted: %v", err)
	}
	s.Close()
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err = os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err = openStageStore(filepath.Join(link, "state"), "daemon", "unix:///tmp/docker.sock"); err == nil || !strings.Contains(err.Error(), "state path") {
		t.Fatalf("symlink admitted: %v", err)
	}
}

func TestLocalStageJournalNeverSkipsUnobservedEffect(t *testing.T) {
	s, err := openStageStore(filepath.Join(t.TempDir(), "state"), "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Begin(strings.Repeat("1", 32), "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err = s.Advance("allocating", "create_volume", "attempted"); err != nil {
		t.Fatal(err)
	}
	if err = s.Advance("candidate_resources", "create_container", "attempted"); err == nil || !strings.Contains(err.Error(), "unobserved effect") {
		t.Fatalf("unobserved effect advanced: %v", err)
	}
	if err = s.Advance("allocating", "create_volume", "uncertain"); err != nil {
		t.Fatal(err)
	}
	if err = s.Advance("allocating", "create_volume", "observed"); err == nil || !strings.Contains(err.Error(), "reconciliation") {
		t.Fatalf("uncertain effect retried: %v", err)
	}
}

func TestLocalStageLostJournalBlocksFreshAttempt(t *testing.T) {
	s, err := openStageStore(filepath.Join(t.TempDir(), "state"), "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	retained := filepath.Join(s.path, strings.Repeat("2", 32))
	if err = os.Mkdir(retained, 0700); err != nil {
		t.Fatal(err)
	}
	if err = s.Begin(strings.Repeat("1", 32), "sha256:"+strings.Repeat("a", 64)); err == nil || !strings.Contains(err.Error(), "retained attempt") {
		t.Fatalf("lost-journal orphan adopted: %v", err)
	}
	if _, err = os.Stat(filepath.Join(s.path, "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("fresh intent written despite retained data: %v", err)
	}
}

func TestLocalStageVerifiedAttemptRequiresReconciliation(t *testing.T) {
	s, err := openStageStore(filepath.Join(t.TempDir(), "state"), "daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	op := strings.Repeat("1", 32)
	digest := "sha256:" + strings.Repeat("a", 64)
	if err = s.Begin(op, digest); err != nil {
		t.Fatal(err)
	}
	for _, state := range stageStates[1:] {
		if err = s.Advance(state, "", "observed"); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Advance("exporting_results", "", "observed"); err == nil || !strings.Contains(err.Error(), "state transition") {
		t.Fatalf("promote-only state admitted: %v", err)
	}
	if err = s.Begin(strings.Repeat("2", 32), digest); err == nil || !strings.Contains(err.Error(), "retained attempt requires owner reconciliation") {
		t.Fatalf("verified stage adopted: %v", err)
	}
}
