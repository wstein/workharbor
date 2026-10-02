package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
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
	if n != 21 || name != "0021_feature_digest.sql" || applied != fixed.UnixNano() {
		t.Errorf("schema_migrations: %d rows, %q at %d", n, name, applied)
	}
	for table, query := range map[string]string{
		"tasks":         `SELECT 1 FROM tasks LIMIT 1`,
		"runs":          `SELECT 1 FROM runs LIMIT 1`,
		"environments":  `SELECT 1 FROM environments LIMIT 1`,
		"candidates":    `SELECT 1 FROM candidates LIMIT 1`,
		"decisions":     `SELECT 1 FROM decisions LIMIT 1`,
		"events":        `SELECT 1 FROM events LIMIT 1`,
		"idempotency":   `SELECT 1 FROM idempotency LIMIT 1`,
		"workspaces":    `SELECT 1 FROM workspaces LIMIT 1`,
		"agents":        `SELECT 1 FROM agents LIMIT 1`,
		"egress_hosts":  `SELECT 1 FROM egress_hosts LIMIT 1`,
		"usage_windows": `SELECT 1 FROM usage_windows LIMIT 1`,
		"audit_chain":   `SELECT 1 FROM audit_chain LIMIT 1`,
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
	if err := s.db.QueryRowContext(bg, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 21 {
		t.Errorf("after a second open: %d migrations recorded, %v; want 21", n, err)
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

// A task keeps the preset it started under, and a change of a repository's preset
// is an audit entry (D47).
func TestWorkflowsAreRecordedAndChangesAreAudited(t *testing.T) {
	s := openTemp(t)
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	if _, ok, err := s.RecordedWorkflow(bg, "a/b"); err != nil || ok {
		t.Fatalf("nothing recorded yet: %v %v", ok, err)
	}
	integration := WorkflowRecord{Workflow: "integration", Branch: "develop"}
	if prev, changed, err := s.ApplyWorkflow(bg, "a/b", integration, "serve", at); err != nil || changed || prev != (WorkflowRecord{}) {
		t.Errorf("the first record is not a change: %q %v %v", prev, changed, err)
	}
	if _, changed, _ := s.ApplyWorkflow(bg, "a/b", integration, "serve", at.Add(time.Hour)); changed {
		t.Error("the same preset was a change")
	}
	// the same repository in another case is the same repository
	if _, changed, _ := s.ApplyWorkflow(bg, "A/B", integration, "serve", at.Add(time.Hour)); changed {
		t.Error("the same repository in capitals was a change")
	}
	if rec, ok, _ := s.RecordedWorkflow(bg, "A/b"); !ok || rec != integration {
		t.Errorf("a name in another case found %+v, %v", rec, ok)
	}
	// a new integration branch under the same preset is a change too
	if prev, changed, _ := s.ApplyWorkflow(bg, "a/b", WorkflowRecord{Workflow: "integration", Branch: "next"}, "host-cli", at.Add(90*time.Minute)); !changed || prev != integration {
		t.Errorf("a new branch: %+v %v", prev, changed)
	}
	if _, _, err := s.ApplyWorkflow(bg, "a/b", integration, "host-cli", at.Add(100*time.Minute)); err != nil {
		t.Fatal(err)
	}
	prev, changed, err := s.ApplyWorkflow(bg, "a/b", WorkflowRecord{Workflow: "prototype", Branch: "dev"}, "host-cli", at.Add(2*time.Hour))
	if err != nil || !changed || prev != integration {
		t.Fatalf("a change: %q %v %v", prev, changed, err)
	}
	if w, _, _ := s.RecordedWorkflow(bg, "a/b"); w != (WorkflowRecord{Workflow: "prototype", Branch: "dev"}) {
		t.Errorf("recorded %q", w)
	}
	ch, err := s.WorkflowChanges(bg, "a/b")
	if err != nil || len(ch) != 3 || ch[0].ToBranch != "next" || ch[2].From != "integration" || ch[2].To != "prototype" || ch[2].FromBranch != "develop" || ch[2].ToBranch != "dev" || ch[2].ConfirmedBy != "host-cli" || !ch[2].At.Equal(at.Add(2*time.Hour)) {
		t.Errorf("changes %+v, %v", ch, err)
	}
	if _, _, err := s.ApplyWorkflow(bg, "", WorkflowRecord{Workflow: "x"}, "y", at); err == nil {
		t.Error("an empty repository was recorded")
	}

	// the task keeps the preset it started under, across a save and a load
	agg := domain.NewTaskAggregate(domain.Task{ID: "t-wf", Repo: "a/b", State: domain.TaskQueued, Workflow: "published", Branch: "develop", CreatedAt: at})
	if _, err := s.SaveTask(bg, agg); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadTask(bg, "t-wf")
	if err == nil && got.Task().Branch != "develop" {
		t.Errorf("task branch %q", got.Task().Branch)
	}
	if err != nil || got.Task().Workflow != "published" {
		t.Errorf("task workflow %q, %v", got.Task().Workflow, err)
	}
}

// An allow of a feature source belongs to the digest the human saw, so one for another
// digest does not count; the latest answer replaces the earlier one.
func TestFeatureSourceAnswersAreKeptPerDigest(t *testing.T) {
	s, err := Open(bg, filepath.Join(t.TempDir(), "workharbor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	const ref = "ghcr.io/someone/else/thing:1"
	d1, d2 := "sha256:"+strings.Repeat("1", 64), "sha256:"+strings.Repeat("2", 64)
	at := time.Unix(100, 0)
	if err := s.SetFeatureSource(bg, "wstein/r", ref, "bad", true, "d1", at); err == nil {
		t.Error("an answer with a malformed digest was kept")
	}
	if err := s.SetFeatureSource(bg, "wstein/r", ref, d1, true, "d1", at); err != nil {
		t.Fatal(err)
	}
	got, err := s.FeatureSources(bg, "wstein/r")
	if err != nil || !got.Allows(ref, d1) || got.Allows(ref, d2) || got.Allows(ref, "") || got.Allows("other", d1) {
		t.Fatalf("answers = %+v, %v", got, err)
	}
	if err := s.SetFeatureSource(bg, "wstein/r", ref, d2, false, "d2", at); err != nil {
		t.Fatal(err)
	}
	got, _ = s.FeatureSources(bg, "wstein/r")
	if got.Allows(ref, d1) || got.Allows(ref, d2) || got[ref].Digest != d2 {
		t.Errorf("after a deny = %+v", got)
	}
	if _, err := s.db.ExecContext(bg, `UPDATE feature_sources SET digest = 'nonsense'`); err == nil {
		t.Error("the table accepted a malformed digest")
	}
}
