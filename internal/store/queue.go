package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/domain"
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

// ErrIssueActive is returned by SaveNewTaskOnce when the issue already has an
// unfinished task.
var ErrIssueActive = errors.New("store: the issue already has an unfinished task")

// SaveNewTaskOnce saves a new task unless an unfinished task of the same repository
// and issue exists, in one transaction: the check and the save cannot interleave
// with another caller's, so two polls of the board queue never both hold an issue.
func (s *Store) SaveNewTaskOnce(ctx context.Context, agg *domain.TaskAggregate) ([]domain.Event, error) {
	var out []domain.Event
	t := agg.Task()
	err := s.Update(ctx, func(tx *Tx) error {
		var n int
		if err := tx.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE lower(repo) = ? AND issue = ?
			AND state NOT IN ('completed', 'cancelled', 'failed')`, repoKey(t.Repo), t.Issue).Scan(&n); err != nil {
			return fmt.Errorf("store: tasks of an issue: %w", err)
		}
		if n > 0 {
			return ErrIssueActive
		}
		var err error
		out, err = tx.SaveTask(ctx, agg)
		return err
	})
	return out, err
}
