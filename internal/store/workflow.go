package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// WorkflowChange is one recorded change of the preset a repository runs under.
type WorkflowChange struct {
	Repo, From, To, ConfirmedBy string
	At                          time.Time
}

// RecordedWorkflow returns the preset a repository was last recorded under, and
// whether there is one.
func (s *Store) RecordedWorkflow(ctx context.Context, repo string) (string, bool, error) {
	var w string
	err := s.db.QueryRowContext(ctx, `SELECT workflow FROM repo_workflows WHERE repo = ?`, repo).Scan(&w)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return w, err == nil, err
}

// ApplyWorkflow records the preset a repository runs under. The first record is
// not a change. A different one is a policy change: it is appended to the audit
// log of changes, with who confirmed it, in the same transaction that replaces
// the recorded preset. It returns the preset it replaced and whether there was a
// change.
func (s *Store) ApplyWorkflow(ctx context.Context, repo, workflow, confirmedBy string, at time.Time) (previous string, changed bool, err error) {
	if repo == "" || workflow == "" {
		return "", false, fmt.Errorf("store: a workflow record needs a repository and a preset")
	}
	err = s.Update(ctx, func(tx *Tx) error {
		qerr := tx.tx.QueryRowContext(ctx, `SELECT workflow FROM repo_workflows WHERE repo = ?`, repo).Scan(&previous)
		switch {
		case errors.Is(qerr, sql.ErrNoRows):
			_, e := tx.tx.ExecContext(ctx, `INSERT INTO repo_workflows (repo, workflow, since) VALUES (?, ?, ?)`, repo, workflow, toNano(at))
			return e
		case qerr != nil:
			return qerr
		case previous == workflow:
			return nil
		}
		changed = true
		if _, e := tx.tx.ExecContext(ctx, `INSERT INTO workflow_changes (repo, from_workflow, to_workflow, confirmed_by, at) VALUES (?, ?, ?, ?, ?)`,
			repo, previous, workflow, confirmedBy, toNano(at)); e != nil {
			return e
		}
		_, e := tx.tx.ExecContext(ctx, `UPDATE repo_workflows SET workflow = ?, since = ? WHERE repo = ?`, workflow, toNano(at), repo)
		return e
	})
	return previous, changed, err
}

// WorkflowChanges returns the recorded changes of a repository, oldest first.
func (s *Store) WorkflowChanges(ctx context.Context, repo string) ([]WorkflowChange, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT repo, from_workflow, to_workflow, confirmed_by, at FROM workflow_changes WHERE repo = ? ORDER BY id`, repo)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WorkflowChange
	for rows.Next() {
		var c WorkflowChange
		var at int64
		if err := rows.Scan(&c.Repo, &c.From, &c.To, &c.ConfirmedBy, &at); err != nil {
			return nil, err
		}
		c.At = fromNano(at)
		out = append(out, c)
	}
	return out, rows.Err()
}
