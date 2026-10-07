package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor/internal/initiation"
)

func TestInitiationSpendAndAuditRollbackTogether(t *testing.T) {
	st := openTemp(t)
	if _, err := st.SaveTask(bg, newAggregate(t, "t1")); err != nil {
		t.Fatal(err)
	}
	record := initiation.Record{ID: "marker", Kind: "user_action", Actor: "user", Channel: "api", At: t0, TaskID: "t1", RunID: "r1", Action: "agent.start"}
	if _, err := st.db.ExecContext(bg, `CREATE TRIGGER refuse_initiation_event BEFORE INSERT ON events WHEN NEW.kind = 'run.initiated' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := st.ConsumeInitiation(bg, record); err == nil {
		t.Fatal("audit failure allowed send")
	}
	var n int
	if err := st.db.QueryRowContext(bg, `SELECT COUNT(*) FROM initiations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed transaction spent marker: %d %v", n, err)
	}
	if _, err := st.db.ExecContext(bg, `DROP TRIGGER refuse_initiation_event`); err != nil {
		t.Fatal(err)
	}
	if err := st.ConsumeInitiation(bg, record); err != nil {
		t.Fatal(err)
	}
	if err := st.ConsumeInitiation(bg, record); !errors.Is(err, initiation.ErrNotInitiated) {
		t.Fatalf("replay: %v", err)
	}
}

func TestInitiationSpendSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spend.db")
	st, err := Open(bg, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveTask(bg, newAggregate(t, "t1")); err != nil {
		t.Fatal(err)
	}
	record := initiation.Record{ID: "marker", Kind: "user_action", Actor: "user", Channel: "api", At: t0, TaskID: "t1", RunID: "r1", Action: "agent.resume"}
	if err := st.ConsumeInitiation(bg, record); err != nil {
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
	if err := st.ConsumeInitiation(bg, record); !errors.Is(err, initiation.ErrNotInitiated) {
		t.Fatalf("restart allowed replay: %v", err)
	}
}
