package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Statuses of a pending change.
const (
	ChangeOpen      = "open"
	ChangeApplied   = "applied"
	ChangeWithdrawn = "withdrawn"
	ChangeStale     = "stale"
)

// Errors of the pending changes.
var (
	// ErrNoChange means no open change has that ID.
	ErrNoChange = errors.New("store: no such open change")
	// ErrStaleChange means the repository no longer runs under what the change was
	// raised against, so confirming it would not be what the human was shown.
	ErrStaleChange = errors.New("store: the repository's workflow changed since this change was raised")
)

// PendingChange is a change of a repository's workflow that the configuration
// asks for and nobody has confirmed (design D47, issue #107).
type PendingChange struct {
	ID        string
	Repo      string
	From, To  WorkflowRecord
	CreatedAt time.Time
}

// Text names the change as a passkey step-up binds it: the repository, what it
// runs under and what it would run under.
func (c PendingChange) Text() string {
	return fmt.Sprintf("workflow %s %s on %q to %s on %q", c.Repo, c.From.Workflow, c.From.Branch, c.To.Workflow, c.To.Branch)
}

// RaiseChange records that a repository's configured workflow differs from the
// recorded one and returns the open change for it. The same change raised again
// (a restart) is the one already open; a different one withdraws the open one,
// so a repository has at most one open change.
func (s *Store) RaiseChange(ctx context.Context, id, repo string, from, to WorkflowRecord, at time.Time) (PendingChange, error) {
	if id == "" || repo == "" || from == to {
		return PendingChange{}, fmt.Errorf("store: a change needs an ID, a repository and a difference")
	}
	key := repoKey(repo)
	out := PendingChange{ID: id, Repo: key, From: from, To: to, CreatedAt: at}
	err := s.Update(ctx, func(tx *Tx) error {
		var cur PendingChange
		var created int64
		qerr := tx.tx.QueryRowContext(ctx, `SELECT id, from_workflow, from_branch, to_workflow, to_branch, created_at FROM pending_changes WHERE repo = ? AND status = 'open'`, key).
			Scan(&cur.ID, &cur.From.Workflow, &cur.From.Branch, &cur.To.Workflow, &cur.To.Branch, &created)
		switch {
		case qerr == nil && cur.From == from && cur.To == to:
			out.ID, out.CreatedAt = cur.ID, fromNano(created)
			return nil
		case qerr == nil:
			if _, e := tx.tx.ExecContext(ctx, `UPDATE pending_changes SET status = 'withdrawn', closed_at = ? WHERE id = ?`, toNano(at), cur.ID); e != nil {
				return e
			}
		case !errors.Is(qerr, sql.ErrNoRows):
			return qerr
		}
		_, e := tx.tx.ExecContext(ctx, `INSERT INTO pending_changes (id, repo, from_workflow, from_branch, to_workflow, to_branch, status, created_at) VALUES (?, ?, ?, ?, ?, ?, 'open', ?)`,
			id, key, from.Workflow, from.Branch, to.Workflow, to.Branch, toNano(at))
		return e
	})
	return out, err
}

// WithdrawChangesExcept withdraws every open change whose repository is not in
// keep: the configuration no longer asks for it. It returns how many.
func (s *Store) WithdrawChangesExcept(ctx context.Context, keep []string, at time.Time) (int, error) {
	kept := map[string]bool{}
	for _, r := range keep {
		kept[repoKey(r)] = true
	}
	open, err := s.OpenChanges(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range open {
		if kept[c.Repo] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE pending_changes SET status = 'withdrawn', closed_at = ? WHERE id = ? AND status = 'open'`, toNano(at), c.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// OpenChanges returns the changes waiting for a confirmation, oldest first.
func (s *Store) OpenChanges(ctx context.Context) ([]PendingChange, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, repo, from_workflow, from_branch, to_workflow, to_branch, created_at FROM pending_changes WHERE status = 'open' ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PendingChange
	for rows.Next() {
		var c PendingChange
		var at int64
		if err := rows.Scan(&c.ID, &c.Repo, &c.From.Workflow, &c.From.Branch, &c.To.Workflow, &c.To.Branch, &at); err != nil {
			return nil, err
		}
		c.CreatedAt = fromNano(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConfirmChange applies an open change and closes it, in one transaction: the
// workflow record, the audit entry of the change with who confirmed it, and the
// status. A change whose repository no longer runs under its "from" is marked
// stale and refused. A closed change cannot be confirmed again.
func (s *Store) ConfirmChange(ctx context.Context, id, by string, at time.Time) (PendingChange, error) {
	var c PendingChange
	var stale bool
	err := s.Update(ctx, func(tx *Tx) error {
		var created int64
		qerr := tx.tx.QueryRowContext(ctx, `SELECT id, repo, from_workflow, from_branch, to_workflow, to_branch, created_at FROM pending_changes WHERE id = ? AND status = 'open'`, id).
			Scan(&c.ID, &c.Repo, &c.From.Workflow, &c.From.Branch, &c.To.Workflow, &c.To.Branch, &created)
		if errors.Is(qerr, sql.ErrNoRows) {
			return ErrNoChange
		}
		if qerr != nil {
			return qerr
		}
		c.CreatedAt = fromNano(created)
		var cur WorkflowRecord
		if e := tx.tx.QueryRowContext(ctx, `SELECT workflow, branch FROM repo_workflows WHERE repo = ?`, c.Repo).Scan(&cur.Workflow, &cur.Branch); e != nil || cur != c.From {
			stale = true
			_, e := tx.tx.ExecContext(ctx, `UPDATE pending_changes SET status = 'stale', closed_at = ?, closed_by = ? WHERE id = ?`, toNano(at), by, id)
			return e
		}
		if _, _, e := applyWorkflowTx(ctx, tx, c.Repo, c.To, by, at); e != nil {
			return e
		}
		_, e := tx.tx.ExecContext(ctx, `UPDATE pending_changes SET status = 'applied', closed_at = ?, closed_by = ? WHERE id = ?`, toNano(at), by, id)
		return e
	})
	if err == nil && stale {
		return c, ErrStaleChange
	}
	return c, err
}
