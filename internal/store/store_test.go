package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

var bg = context.Background()

func openTemp(t *testing.T, opts ...Option) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "workharbor.db"), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenUsesWALAndForeignKeys(t *testing.T) {
	s := openTemp(t)
	var mode string
	if err := s.db.QueryRowContext(bg, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v; want wal", mode, err)
	}
	var fk int
	if err := s.db.QueryRowContext(bg, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want 3", fk, err)
	}
	// A child row without its parent is refused.
	if _, err := s.db.ExecContext(bg, `INSERT INTO runs (id, task_id, workspace_id, env_id, state, ord) VALUES ('r', 'no-task', 'w', 'e', 'starting', 0)`); err == nil {
		t.Error("a run for an unknown task must be refused: foreign keys are off")
	}
}

func TestMigrationsAreAppliedOnceAndRecorded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workharbor.db")
	ctx := context.Background()
	fixed := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	s, err := Open(ctx, path, WithClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var name string
	var applied int64
	if err := s.db.QueryRowContext(bg, `SELECT COUNT(*), MAX(name), MAX(applied_at) FROM schema_migrations`).Scan(&n, &name, &applied); err != nil {
		t.Fatal(err)
	}
	if n != 7 || name != "0007_egress_hosts.sql" || applied != fixed.UnixNano() {
		t.Errorf("schema_migrations: %d rows, %q at %d", n, name, applied)
	}
	for table, query := range map[string]string{
		"tasks":        `SELECT 1 FROM tasks LIMIT 1`,
		"runs":         `SELECT 1 FROM runs LIMIT 1`,
		"environments": `SELECT 1 FROM environments LIMIT 1`,
		"candidates":   `SELECT 1 FROM candidates LIMIT 1`,
		"decisions":    `SELECT 1 FROM decisions LIMIT 1`,
		"events":       `SELECT 1 FROM events LIMIT 1`,
		"idempotency":  `SELECT 1 FROM idempotency LIMIT 1`,
		"workspaces":   `SELECT 1 FROM workspaces LIMIT 1`,
		"agents":       `SELECT 1 FROM agents LIMIT 1`,
		"egress_hosts": `SELECT 1 FROM egress_hosts LIMIT 1`,
	} {
		rows, err := s.db.QueryContext(bg, query)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
			continue
		}
		_ = rows.Close()
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Opening again applies nothing new.
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.db.QueryRowContext(bg, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 7 {
		t.Errorf("after a second open: %d migrations recorded, %v; want 7", n, err)
	}
}

func TestADatabaseFromANewerVersionIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workharbor.db")
	ctx := context.Background()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(bg, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (999, '0999_future.sql', 0)`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	if _, err := Open(ctx, path); !errors.Is(err, ErrNewerDatabase) {
		t.Errorf("Open of a newer database = %v, want ErrNewerDatabase", err)
	}
}

func TestOpenFailsOnAnUnusablePath(t *testing.T) {
	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "missing", "dir", "x.db")); err == nil {
		t.Error("Open below a directory that does not exist must fail")
	}
}

func TestLoadMigrationsAreOrderedAndNamed(t *testing.T) {
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 || ms[0].version != 1 {
		t.Fatalf("migrations = %+v", ms)
	}
	for i := 1; i < len(ms); i++ {
		if ms[i].version <= ms[i-1].version {
			t.Errorf("migrations are not in order: %d after %d", ms[i].version, ms[i-1].version)
		}
	}
}
