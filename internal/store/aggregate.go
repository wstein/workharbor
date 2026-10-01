package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// RuleStale is the conflict rule of a lost compare-and-swap.
const RuleStale domain.Rule = "stale"

// ErrStale is returned when a save loses a compare-and-swap: another writer
// changed the task or Decision after it was loaded. The caller reloads and
// decides again. It is a conflict (exit code 5).
var ErrStale = domain.NewConflict(RuleStale, "the record was changed by someone else since it was loaded")

// ErrNoEvents is returned when a save would change state that no recorded
// event accounts for. Every change goes through the aggregate, which records
// it, so such a save is a forgery or a bug, and it is refused (design §5.4).
var ErrNoEvents = errors.New("store: a change of state with no recorded event")

// ErrNoDeadline is returned for an approval without a deadline, which is
// never written and never read back: every approval has one, so a missing one
// is a lost value, and acting on it could let a late allow through (design
// §4.2). It is an ordinary error (exit code 1), not a conflict.
var ErrNoDeadline = errors.New("store: an approval has no deadline")

func toNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromNano(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SaveTask writes a task aggregate and the events its changes recorded, in
// this transaction. The write is a compare-and-swap on the task's Version: a
// new aggregate (version 0) is inserted, a loaded one is updated only if
// nobody else changed it, otherwise the save is ErrStale and nothing is
// written. Its runs, environments and review candidates are written with it.
// After the transaction commits the aggregate's Version moves and its recorded
// events are forgotten; the returned events carry their sequence numbers.
func (tx *Tx) SaveTask(ctx context.Context, agg *domain.TaskAggregate) ([]domain.Event, error) {
	rd := tx.s.redactor
	snap := agg.Snapshot()
	t := &snap.Task
	expected := t.Version
	if expected == 0 {
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO tasks (id, version, repo, issue, state, created_at) VALUES (?, 1, ?, ?, ?, ?)`,
			string(t.ID), rd.String(t.Repo), rd.String(t.Issue), string(t.State), toNano(t.CreatedAt)); err != nil {
			var exists int
			if tx.tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, string(t.ID)).Scan(&exists) == nil {
				return nil, fmt.Errorf("task %s: %w", t.ID, ErrStale)
			}
			return nil, fmt.Errorf("store: save task %s: %w", t.ID, err)
		}
	} else {
		if len(agg.PendingEvents()) == 0 {
			stored, err := tx.LoadTask(ctx, t.ID)
			if err != nil {
				return nil, err
			}
			if stored.Task().Version == expected && !stored.Snapshot().SameState(snap) {
				return nil, fmt.Errorf("task %s: %w", t.ID, ErrNoEvents)
			}
		}
		res, err := tx.tx.ExecContext(ctx, `UPDATE tasks SET repo = ?, issue = ?, state = ?, version = version + 1 WHERE id = ? AND version = ?`,
			rd.String(t.Repo), rd.String(t.Issue), string(t.State), string(t.ID), expected)
		if err != nil {
			return nil, fmt.Errorf("store: save task %s: %w", t.ID, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var exists int
			if errors.Is(tx.tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, string(t.ID)).Scan(&exists), sql.ErrNoRows) {
				return nil, &domain.NotFoundError{Kind: "task", ID: string(t.ID)}
			}
			return nil, fmt.Errorf("task %s: %w", t.ID, ErrStale)
		}
	}

	for _, table := range []string{"runs", "environments", "candidates"} {
		if _, err := tx.tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE task_id = ?`, string(t.ID)); err != nil { //nolint:gosec // table is one of three constants
			return nil, fmt.Errorf("store: save task %s: %w", t.ID, err)
		}
	}
	for _, e := range snap.Envs {
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO environments (id, task_id, backend, state) VALUES (?, ?, ?, ?)`,
			string(e.ID), string(t.ID), e.Backend, string(e.State)); err != nil {
			return nil, fmt.Errorf("store: save environment %s: %w", e.ID, err)
		}
	}
	for i, r := range snap.Runs {
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO runs (id, task_id, workspace_id, env_id, state, session_id, resume_attempts, ord) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(r.ID), string(t.ID), string(r.WorkspaceID), string(r.EnvID), string(r.State), r.SessionID, r.ResumeAttempts, i); err != nil {
			return nil, fmt.Errorf("store: save run %s: %w", r.ID, err)
		}
	}
	for i, c := range snap.Candidates {
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO candidates (task_id, sha, run_id, branch, pr_url, ci, ord) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(t.ID), c.SHA, string(c.RunID), rd.String(c.Branch), rd.String(c.PRURL), string(c.CI), i); err != nil {
			return nil, fmt.Errorf("store: save candidate %s: %w", c.SHA, err)
		}
	}

	// Decisions that changed are written with the task. Their events are
	// already among the aggregate's.
	versions := map[domain.ID]int64{}
	for i := range snap.Decisions {
		d := &snap.Decisions[i].Decision
		if d.Version != 0 && !snap.Decisions[i].Changed {
			continue
		}
		next, err := tx.writeDecision(ctx, d)
		if err != nil {
			return nil, err
		}
		versions[d.ID] = next
	}

	events, err := tx.Append(ctx, agg.PendingEvents()...)
	if err != nil {
		return nil, err
	}
	tx.after = append(tx.after, func() { agg.MarkSaved(expected+1, versions) })
	return events, nil
}

// LoadTask reads a task aggregate with its runs, environments and review
// candidates. An unknown task is a *domain.NotFoundError.
func (tx *Tx) LoadTask(ctx context.Context, id domain.ID) (*domain.TaskAggregate, error) {
	var t domain.Task
	var state string
	var created int64
	err := tx.tx.QueryRowContext(ctx, `SELECT version, repo, issue, state, created_at FROM tasks WHERE id = ?`, string(id)).
		Scan(&t.Version, &t.Repo, &t.Issue, &state, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &domain.NotFoundError{Kind: "task", ID: string(id)}
	}
	if err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}
	t.ID, t.State, t.CreatedAt = id, domain.TaskState(state), fromNano(created)
	snap := domain.Snapshot{Task: t}

	envs, err := tx.tx.QueryContext(ctx, `SELECT id, backend, state FROM environments WHERE task_id = ? ORDER BY id`, string(id))
	if err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}
	for envs.Next() {
		var e domain.Environment
		var eid, st string
		if err := envs.Scan(&eid, &e.Backend, &st); err != nil {
			_ = envs.Close()
			return nil, fmt.Errorf("store: load task %s: %w", id, err)
		}
		e.ID, e.State = domain.ID(eid), domain.EnvState(st)
		snap.Envs = append(snap.Envs, e)
	}
	if err := envs.Close(); err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}

	runs, err := tx.tx.QueryContext(ctx, `SELECT id, workspace_id, env_id, state, session_id, resume_attempts FROM runs WHERE task_id = ? ORDER BY ord`, string(id))
	if err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}
	for runs.Next() {
		var r domain.Run
		var rid, ws, env, st string
		if err := runs.Scan(&rid, &ws, &env, &st, &r.SessionID, &r.ResumeAttempts); err != nil {
			_ = runs.Close()
			return nil, fmt.Errorf("store: load task %s: %w", id, err)
		}
		r.ID, r.TaskID, r.WorkspaceID, r.EnvID, r.State = domain.ID(rid), id, domain.ID(ws), domain.ID(env), domain.RunState(st)
		snap.Runs = append(snap.Runs, r)
	}
	if err := runs.Close(); err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}

	cands, err := tx.tx.QueryContext(ctx, `SELECT sha, run_id, branch, pr_url, ci FROM candidates WHERE task_id = ? ORDER BY ord`, string(id))
	if err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}
	for cands.Next() {
		c := domain.ReviewCandidate{TaskID: id}
		var run, ci string
		if err := cands.Scan(&c.SHA, &run, &c.Branch, &c.PRURL, &ci); err != nil {
			_ = cands.Close()
			return nil, fmt.Errorf("store: load task %s: %w", id, err)
		}
		c.RunID, c.CI = domain.ID(run), domain.CIState(ci)
		snap.Candidates = append(snap.Candidates, c)
	}
	if err := cands.Close(); err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}

	decs, err := tx.tx.QueryContext(ctx, `SELECT `+decisionColumns+` FROM decisions WHERE task_id = ? ORDER BY created_at, id`, string(id))
	if err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}
	for decs.Next() {
		d, err := scanDecision(decs)
		if err != nil {
			_ = decs.Close()
			return nil, fmt.Errorf("store: load task %s: %w", id, err)
		}
		snap.Decisions = append(snap.Decisions, domain.DecisionState{Decision: *d})
	}
	if err := decs.Close(); err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}
	agg, err := domain.Restore(snap)
	if err != nil {
		return nil, fmt.Errorf("store: load task %s: %w", id, err)
	}
	return agg, nil
}

// writeDecision writes the Decision row with a compare-and-swap on its
// version, and returns the version it has once the transaction commits.
func (tx *Tx) writeDecision(ctx context.Context, d *domain.Decision) (int64, error) {
	if d.Kind == domain.DecisionApproval && d.Deadline.IsZero() {
		return 0, fmt.Errorf("decision %s: %w", d.ID, ErrNoDeadline)
	}
	rd := tx.s.redactor
	redactedOptions := make([]string, len(d.Options))
	for i, o := range d.Options {
		redactedOptions[i] = rd.String(o)
	}
	options, err := json.Marshal(redactedOptions)
	if err != nil {
		return 0, fmt.Errorf("store: save decision %s: %w", d.ID, err)
	}
	var answeredAt int64
	if d.AnsweredAt != nil {
		answeredAt = toNano(*d.AnsweredAt)
	}
	expected := d.Version
	values := []any{
		string(d.TaskID), string(d.RunID), string(d.Kind), boolInt(d.Blocking), rd.String(d.Subject), rd.String(d.Input), boolInt(d.InputTruncated),
		d.SHA, string(options), string(d.Status), toNano(d.CreatedAt), int64(d.Timeout), toNano(d.Deadline), answeredAt,
		rd.String(d.Answer), rd.String(d.Reason), rd.String(d.AnsweredBy), string(d.SupersededBy), string(d.Cause), toNano(d.ResumeAt),
	}
	if expected == 0 {
		args := append([]any{string(d.ID)}, values...)
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO decisions (id, version, task_id, run_id, kind, blocking, subject, input, input_truncated,
			sha, options, status, created_at, timeout_ns, deadline, answered_at, answer, reason, answered_by, superseded_by, cause, resume_at)
			VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
			var exists int
			if tx.tx.QueryRowContext(ctx, `SELECT 1 FROM decisions WHERE id = ?`, string(d.ID)).Scan(&exists) == nil {
				return 0, fmt.Errorf("decision %s: %w", d.ID, ErrStale)
			}
			return 0, fmt.Errorf("store: save decision %s: %w", d.ID, err)
		}
	} else {
		args := append(values, string(d.ID), expected)
		res, err := tx.tx.ExecContext(ctx, `UPDATE decisions SET version = version + 1, task_id = ?, run_id = ?, kind = ?, blocking = ?, subject = ?,
			input = ?, input_truncated = ?, sha = ?, options = ?, status = ?, created_at = ?, timeout_ns = ?, deadline = ?, answered_at = ?,
			answer = ?, reason = ?, answered_by = ?, superseded_by = ?, cause = ?, resume_at = ? WHERE id = ? AND version = ?`, args...)
		if err != nil {
			return 0, fmt.Errorf("store: save decision %s: %w", d.ID, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var exists int
			if errors.Is(tx.tx.QueryRowContext(ctx, `SELECT 1 FROM decisions WHERE id = ?`, string(d.ID)).Scan(&exists), sql.ErrNoRows) {
				return 0, &domain.NotFoundError{Kind: "decision", ID: string(d.ID)}
			}
			return 0, fmt.Errorf("decision %s: %w", d.ID, ErrStale)
		}
	}
	return expected + 1, nil
}

const decisionColumns = `id, version, task_id, run_id, kind, blocking, subject, input, input_truncated, sha, options, status,
	created_at, timeout_ns, deadline, answered_at, answer, reason, answered_by, superseded_by, cause, resume_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanDecision(r rowScanner) (*domain.Decision, error) {
	var d domain.Decision
	var id, task, run, kind, status, options, superseded, cause string
	var blocking, truncated int
	var created, timeout, deadline, answeredAt, resumeAt int64
	if err := r.Scan(&id, &d.Version, &task, &run, &kind, &blocking, &d.Subject, &d.Input, &truncated, &d.SHA, &options, &status,
		&created, &timeout, &deadline, &answeredAt, &d.Answer, &d.Reason, &d.AnsweredBy, &superseded, &cause, &resumeAt); err != nil {
		return nil, err
	}
	d.ID, d.TaskID, d.RunID, d.Kind, d.Status = domain.ID(id), domain.ID(task), domain.ID(run), domain.DecisionKind(kind), domain.DecisionStatus(status)
	d.Blocking, d.InputTruncated = blocking != 0, truncated != 0
	d.CreatedAt, d.Timeout, d.Deadline, d.SupersededBy = fromNano(created), time.Duration(timeout), fromNano(deadline), domain.ID(superseded)
	d.Cause, d.ResumeAt = domain.DecisionCause(cause), fromNano(resumeAt)
	if answeredAt != 0 {
		at := fromNano(answeredAt)
		d.AnsweredAt = &at
	}
	if err := json.Unmarshal([]byte(options), &d.Options); err != nil {
		return nil, err
	}
	if len(d.Options) == 0 {
		d.Options = nil
	}
	if d.Kind == domain.DecisionApproval && d.Deadline.IsZero() {
		return nil, fmt.Errorf("decision %s: %w", d.ID, ErrNoDeadline)
	}
	return &d, nil
}

// LoadDecision reads a Decision. An unknown one is a *domain.NotFoundError.
func (tx *Tx) LoadDecision(ctx context.Context, id domain.ID) (*domain.Decision, error) {
	d, err := scanDecision(tx.tx.QueryRowContext(ctx, `SELECT `+decisionColumns+` FROM decisions WHERE id = ?`, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &domain.NotFoundError{Kind: "decision", ID: string(id)}
	}
	if err != nil {
		return nil, fmt.Errorf("store: load decision %s: %w", id, err)
	}
	return d, nil
}

// SaveTask saves a task aggregate and its events in one transaction.
func (s *Store) SaveTask(ctx context.Context, agg *domain.TaskAggregate) ([]domain.Event, error) {
	var out []domain.Event
	err := s.Update(ctx, func(tx *Tx) error {
		var err error
		out, err = tx.SaveTask(ctx, agg)
		return err
	})
	return out, err
}

// LoadTask reads a task aggregate.
func (s *Store) LoadTask(ctx context.Context, id domain.ID) (*domain.TaskAggregate, error) {
	var agg *domain.TaskAggregate
	err := s.Update(ctx, func(tx *Tx) error {
		var err error
		agg, err = tx.LoadTask(ctx, id)
		return err
	})
	return agg, err
}

// LoadDecision reads a Decision.
func (s *Store) LoadDecision(ctx context.Context, id domain.ID) (*domain.Decision, error) {
	var d *domain.Decision
	err := s.Update(ctx, func(tx *Tx) error {
		var err error
		d, err = tx.LoadDecision(ctx, id)
		return err
	})
	return d, err
}

// RespondDecision answers a Decision through its task aggregate, in one
// transaction: it loads the Decision's task, calls Answer and saves what
// changed. A refusal that changed something is saved too: a late answer
// expires the Decision, and an allow for another commit is stored as a denial,
// although both return an error (design §4.2). Without this, a caller that
// stopped at the error would lose the change and the Decision would stay open.
// The Decision and the events are returned with the error when something was
// saved. A refusal that changed nothing, such as bad input or a closed
// Decision, returns only the error and writes nothing.
func (s *Store) RespondDecision(ctx context.Context, id domain.ID, r domain.Response) (*domain.Decision, []domain.Event, error) {
	var (
		d       *domain.Decision
		saved   *domain.TaskAggregate
		events  []domain.Event
		respond error
	)
	err := s.Update(ctx, func(tx *Tx) error {
		row, err := tx.LoadDecision(ctx, id)
		if err != nil {
			return err
		}
		agg, err := tx.LoadTask(ctx, row.TaskID)
		if err != nil {
			return err
		}
		respond = agg.Answer(id, r)
		if len(agg.PendingEvents()) == 0 {
			return nil // nothing changed, so nothing to save
		}
		if events, err = tx.SaveTask(ctx, agg); err != nil {
			return err
		}
		saved = agg
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if saved != nil { // read after the commit, which gave the Decision its new version
		if c, ok := saved.Decision(id); ok {
			d = &c
		}
	}
	return d, events, respond
}

// ActiveTaskIDs returns the IDs of the tasks that are not over (completed,
// cancelled or failed), oldest first. The reconciler walks them (design §5.3).
func (s *Store) ActiveTaskIDs(ctx context.Context) ([]domain.ID, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tasks WHERE state NOT IN ('completed', 'cancelled', 'failed') ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: active tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: active tasks: %w", err)
		}
		out = append(out, domain.ID(id))
	}
	return out, rows.Err()
}

// OpenDecisions returns the open Decisions of a task, oldest first. After a
// restart the reconciler supersedes those raised by a run (design §4.2).
func (s *Store) OpenDecisions(ctx context.Context, task domain.ID) ([]*domain.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+decisionColumns+` FROM decisions WHERE task_id = ? AND status = 'open' ORDER BY created_at, id`, string(task))
	if err != nil {
		return nil, fmt.Errorf("store: open decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*domain.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("store: open decisions: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
