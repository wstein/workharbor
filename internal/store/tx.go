package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Tx is a transaction on the store. Everything written through it commits or
// rolls back together, so a state change and its audit events never part.
type Tx struct {
	tx *sql.Tx
	s  *Store
}

// Update runs fn in a transaction: it commits if fn returns nil and rolls
// back otherwise.
func (s *Store) Update(ctx context.Context, fn func(tx *Tx) error) error {
	t, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	if err := fn(&Tx{tx: t, s: s}); err != nil {
		_ = t.Rollback()
		return err
	}
	if err := t.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
