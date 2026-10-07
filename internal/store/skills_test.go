package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

func TestSkillMigrationMarksLegacyWithoutRewritingEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.db")
	st, err := Open(bg, path)
	if err != nil {
		t.Fatal(err)
	}
	aggregate := newAggregate(t, "t1")
	if _, err := st.SaveTask(bg, aggregate); err != nil {
		t.Fatal(err)
	}
	before, err := st.EventsSince(bg, "t1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"admitted_at", "ended_at", "duration_ms", "duration_at", "terminal_reason"} {
		if _, err := st.db.ExecContext(bg, `ALTER TABLE runs DROP COLUMN `+column); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.ExecContext(bg, `ALTER TABLE runs DROP COLUMN skills`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(bg, `DROP TABLE initiations`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(bg, `DELETE FROM schema_migrations WHERE version >= 22`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(bg, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	loaded, err := st.LoadTask(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runs()[0].Skills.Mode != "legacy" {
		t.Fatal("legacy provenance not explicit")
	}
	after, err := st.EventsSince(bg, "t1", 0, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration rewrote existing events")
	}
}

func TestSkillProvenanceRoundTripsAndMalformedRecordsRefuseLoad(t *testing.T) {
	st := openTemp(t)
	aggregate := newAggregate(t, "t1")
	if _, err := st.SaveTask(bg, aggregate); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(bg, `UPDATE runs SET skills = ? WHERE task_id = 't1'`, `{"mode":"none","project_sha256":"abc","instruction_sha256":"def"}`); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadTask(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Runs()[0].Skills; got != (domain.SkillSelection{Mode: "none", ProjectSHA256: "abc", InstructionSHA256: "def"}) {
		t.Fatalf("provenance: %+v", got)
	}
	if err := loaded.MarkRunning("r-t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveTask(bg, loaded); err != nil {
		t.Fatal(err)
	}
	again, err := st.LoadTask(bg, "t1")
	if err != nil || !reflect.DeepEqual(loaded.Snapshot(), again.Snapshot()) {
		t.Fatalf("record lost on save: %v", err)
	}
	if _, err := st.db.ExecContext(bg, `UPDATE runs SET skills = 'invalid'`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadTask(bg, "t1"); err == nil {
		t.Fatal("invalid provenance silently became legacy")
	}
}
