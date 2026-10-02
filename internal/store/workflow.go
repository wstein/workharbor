package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WorkflowRecord is what a repository runs under: its preset and the integration
// branch approved commits go to (empty for a published repository).
type WorkflowRecord struct {
	Workflow, Branch string
}

// WorkflowChange is one recorded change of the preset or the integration branch a
// repository runs under.
type WorkflowChange struct {
	Repo, From, To, FromBranch, ToBranch, ConfirmedBy string
	At                                                time.Time
}

// repoKey is how a repository is looked up: without regard to case, as the
// forge treats names, so the same repository written in another case is not a
// new one.
func repoKey(repo string) string { return strings.ToLower(repo) }

// RecordedWorkflow returns what a repository was last recorded under, and whether
// there is a record.
func (s *Store) RecordedWorkflow(ctx context.Context, repo string) (WorkflowRecord, bool, error) {
	var r WorkflowRecord
	err := s.db.QueryRowContext(ctx, `SELECT workflow, branch FROM repo_workflows WHERE repo = ?`, repoKey(repo)).Scan(&r.Workflow, &r.Branch)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkflowRecord{}, false, nil
	}
	return r, err == nil, err
}

// ApplyWorkflow records what a repository runs under. The first record is not a
// change. A different preset or integration branch is a policy change: it is
// appended to the audit log of changes, with who confirmed it, in the same
// transaction that replaces the record. It returns what it replaced and whether
// there was a change.
func (s *Store) ApplyWorkflow(ctx context.Context, repo string, rec WorkflowRecord, confirmedBy string, at time.Time) (previous WorkflowRecord, changed bool, err error) {
	if repo == "" || rec.Workflow == "" {
		return WorkflowRecord{}, false, fmt.Errorf("store: a workflow record needs a repository and a preset")
	}
	key := repoKey(repo)
	err = s.Update(ctx, func(tx *Tx) error {
		qerr := tx.tx.QueryRowContext(ctx, `SELECT workflow, branch FROM repo_workflows WHERE repo = ?`, key).Scan(&previous.Workflow, &previous.Branch)
		switch {
		case errors.Is(qerr, sql.ErrNoRows):
			_, e := tx.tx.ExecContext(ctx, `INSERT INTO repo_workflows (repo, workflow, branch, since) VALUES (?, ?, ?, ?)`, key, rec.Workflow, rec.Branch, toNano(at))
			return e
		case qerr != nil:
			return qerr
		case previous == rec:
			return nil
		}
		changed = true
		if _, e := tx.tx.ExecContext(ctx, `INSERT INTO workflow_changes (repo, from_workflow, to_workflow, from_branch, to_branch, confirmed_by, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			key, previous.Workflow, rec.Workflow, previous.Branch, rec.Branch, confirmedBy, toNano(at)); e != nil {
			return e
		}
		_, e := tx.tx.ExecContext(ctx, `UPDATE repo_workflows SET workflow = ?, branch = ?, since = ? WHERE repo = ?`, rec.Workflow, rec.Branch, toNano(at), key)
		return e
	})
	return previous, changed, err
}

// WorkflowChanges returns the recorded changes of a repository, oldest first.
func (s *Store) WorkflowChanges(ctx context.Context, repo string) ([]WorkflowChange, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT repo, from_workflow, to_workflow, from_branch, to_branch, confirmed_by, at FROM workflow_changes WHERE repo = ? ORDER BY id`, repoKey(repo))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WorkflowChange
	for rows.Next() {
		var c WorkflowChange
		var at int64
		if err := rows.Scan(&c.Repo, &c.From, &c.To, &c.FromBranch, &c.ToBranch, &c.ConfirmedBy, &at); err != nil {
			return nil, err
		}
		c.At = fromNano(at)
		out = append(out, c)
	}
	return out, rows.Err()
}
