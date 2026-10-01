// Package store persists tasks, runs, decisions and the event log in SQLite
// (design §5.4). It uses the pure-Go driver modernc.org/sqlite, so the
// supervisor stays one static binary that cross-compiles; the driver is the
// only new dependency, and cgo is not needed.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrNewerDatabase is returned when the database was written by a newer
// version of workharbor than this binary knows.
var ErrNewerDatabase = errors.New("the database was written by a newer version of workharbor")

// Store is a SQLite database of tasks, decisions and events.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Option configures Open.
type Option func(*Store)

// WithClock sets the clock used to stamp events that have no time. Tests use
// it to be deterministic.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// Open opens the database at path, creating it if needed, in WAL mode with
// foreign keys on, and applies the migrations it has not seen. A single
// connection serialises writers, which is what one host and one user need
// and keeps compare-and-swap simple.
func Open(ctx context.Context, path string, opts ...Option) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	dsn := (&url.URL{Scheme: "file", Opaque: path, RawQuery: q.Encode()}).String()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	for _, o := range opts {
		o(s)
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(prefix)
		if !ok || err != nil {
			return nil, fmt.Errorf("store: migration %q must start with a version number and an underscore", e.Name())
		}
		body, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	var current sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 || (current.Valid && int(current.Int64) > migrations[len(migrations)-1].version) {
		return fmt.Errorf("store: %w (database version %d)", ErrNewerDatabase, current.Int64)
	}
	for _, m := range migrations {
		if current.Valid && m.version <= int(current.Int64) {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: %w", err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, s.now().UnixNano()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: %w", err)
		}
	}
	return nil
}
