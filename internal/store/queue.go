package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// QueueSeen returns when a card of the board's agent queue was last changed as of the
// last time it was acted on, and whether it was seen at all.
func (s *Store) QueueSeen(ctx context.Context, repo string, issue int) (time.Time, bool, error) {
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM board_queue WHERE repo = ? AND issue = ?`, repoKey(repo), issue).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: queue: %w", err)
	}
	return fromNano(at), true, nil
}

// RememberQueue records that a card in the state of updatedAt was acted on, and the
// task it raised, if any.
func (s *Store) RememberQueue(ctx context.Context, repo string, issue int, updatedAt time.Time, task string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO board_queue (repo, issue, updated_at, task_id) VALUES (?, ?, ?, ?)
		ON CONFLICT (repo, issue) DO UPDATE SET updated_at = excluded.updated_at, task_id = excluded.task_id`,
		repoKey(repo), issue, toNano(updatedAt), task)
	if err != nil {
		return fmt.Errorf("store: queue: %w", err)
	}
	return nil
}
