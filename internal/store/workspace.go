package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// Rules of the registry: a name or role that is taken, and a record that is
// still in use.
const (
	RuleExists Rule = "exists" // the name, path or role is already taken
	RuleInUse  Rule = "in-use" // the record is still referenced
)

// Rule aliases domain.Rule so the constants above read as the store's own.
type Rule = domain.Rule

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// AddWorkspace writes a workspace and its creation event in one transaction. A
// name or path that is taken is a conflict.
func (s *Store) AddWorkspace(ctx context.Context, w domain.Workspace, ev domain.Event) error {
	return s.Update(ctx, func(tx *Tx) error {
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO workspaces (id, name, path, repo, integration, env_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(w.ID), w.Name, w.Path, w.Repo, w.Integration, string(w.EnvID), toNano(w.CreatedAt)); err != nil {
			if isUnique(err) {
				return domain.NewConflict(RuleExists, "a workspace with the name %q or the path %q already exists", w.Name, w.Path)
			}
			return fmt.Errorf("store: add workspace: %w", err)
		}
		_, err := tx.Append(ctx, ev)
		return err
	})
}

const workspaceColumns = `id, name, path, repo, integration, env_id, created_at`

func scanWorkspace(row interface{ Scan(...any) error }) (domain.Workspace, error) {
	var w domain.Workspace
	var id, env string
	var created int64
	if err := row.Scan(&id, &w.Name, &w.Path, &w.Repo, &w.Integration, &env, &created); err != nil {
		return domain.Workspace{}, err
	}
	w.ID, w.EnvID, w.CreatedAt = domain.ID(id), domain.ID(env), fromNano(created)
	return w, nil
}

// Workspace returns the workspace with this ID or name. An unknown one is a
// *domain.NotFoundError.
func (s *Store) Workspace(ctx context.Context, idOrName string) (domain.Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRowContext(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = ? OR name = ?`, idOrName, idOrName)) //nolint:gosec // column list is a constant
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Workspace{}, &domain.NotFoundError{Kind: "workspace", ID: idOrName}
	}
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("store: workspace %s: %w", idOrName, err)
	}
	return w, nil
}

// Workspaces lists every workspace by name.
func (s *Store) Workspaces(ctx context.Context) ([]domain.Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workspaceColumns+` FROM workspaces ORDER BY name`) //nolint:gosec // column list is a constant
	if err != nil {
		return nil, fmt.Errorf("store: workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("store: workspaces: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SetWorkspaceEnv records the environment a workspace runs in.
func (s *Store) SetWorkspaceEnv(ctx context.Context, id, env domain.ID) error {
	res, err := s.db.ExecContext(ctx, `UPDATE workspaces SET env_id = ? WHERE id = ?`, string(env), string(id))
	if err != nil {
		return fmt.Errorf("store: set workspace environment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &domain.NotFoundError{Kind: "workspace", ID: string(id)}
	}
	return nil
}

// RemoveWorkspace deletes a workspace record and appends the event. A workspace
// that still has agents is a conflict: the human removes them first.
func (s *Store) RemoveWorkspace(ctx context.Context, w domain.Workspace, ev domain.Event) error {
	return s.Update(ctx, func(tx *Tx) error {
		if _, err := tx.tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, string(w.ID)); err != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
				return domain.NewConflict(RuleInUse, "workspace %s still has agents: remove them first", w.Name)
			}
			return fmt.Errorf("store: remove workspace: %w", err)
		}
		_, err := tx.Append(ctx, ev)
		return err
	})
}

// AddAgent writes an agent and its creation event. A role that is taken in the
// workspace is a conflict; an unknown workspace is not found.
func (s *Store) AddAgent(ctx context.Context, a domain.Agent, ev domain.Event) error {
	return s.Update(ctx, func(tx *Tx) error {
		instr := s.redactor.String(a.Instructions)
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO agents (id, workspace_id, role, branch, worktree, instructions, profile, session_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(a.ID), string(a.WorkspaceID), a.Role, a.Branch, a.Worktree, instr, a.Profile, a.SessionID); err != nil {
			switch {
			case isUnique(err):
				return domain.NewConflict(RuleExists, "workspace %s already has an agent %q", a.WorkspaceID, a.Role)
			case strings.Contains(err.Error(), "FOREIGN KEY constraint failed"):
				return &domain.NotFoundError{Kind: "workspace", ID: string(a.WorkspaceID)}
			}
			return fmt.Errorf("store: add agent: %w", err)
		}
		_, err := tx.Append(ctx, ev)
		return err
	})
}

const agentColumns = `id, workspace_id, role, branch, worktree, instructions, profile, session_id`

func scanAgent(row interface{ Scan(...any) error }) (domain.Agent, error) {
	var a domain.Agent
	var id, ws string
	if err := row.Scan(&id, &ws, &a.Role, &a.Branch, &a.Worktree, &a.Instructions, &a.Profile, &a.SessionID); err != nil {
		return domain.Agent{}, err
	}
	a.ID, a.WorkspaceID = domain.ID(id), domain.ID(ws)
	return a, nil
}

// Agent returns the agent with this ID. An unknown one is a *domain.NotFoundError.
func (s *Store) Agent(ctx context.Context, id domain.ID) (domain.Agent, error) {
	a, err := scanAgent(s.db.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = ?`, string(id))) //nolint:gosec // column list is a constant
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Agent{}, &domain.NotFoundError{Kind: "agent", ID: string(id)}
	}
	if err != nil {
		return domain.Agent{}, fmt.Errorf("store: agent %s: %w", id, err)
	}
	return a, nil
}

// AgentByRole returns the agent of a workspace with this role.
func (s *Store) AgentByRole(ctx context.Context, workspace domain.ID, role string) (domain.Agent, error) {
	a, err := scanAgent(s.db.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE workspace_id = ? AND role = ?`, string(workspace), role)) //nolint:gosec // column list is a constant
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Agent{}, &domain.NotFoundError{Kind: "agent", ID: role}
	}
	if err != nil {
		return domain.Agent{}, fmt.Errorf("store: agent %s: %w", role, err)
	}
	return a, nil
}

// Agents lists a workspace's agents by role.
func (s *Store) Agents(ctx context.Context, workspace domain.ID) ([]domain.Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE workspace_id = ? ORDER BY role`, string(workspace)) //nolint:gosec // column list is a constant
	if err != nil {
		return nil, fmt.Errorf("store: agents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("store: agents: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAgentSession records the agent's current session.
func (s *Store) SetAgentSession(ctx context.Context, id domain.ID, session string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agents SET session_id = ? WHERE id = ?`, session, string(id))
	if err != nil {
		return fmt.Errorf("store: set agent session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &domain.NotFoundError{Kind: "agent", ID: string(id)}
	}
	return nil
}

// RemoveAgent deletes an agent and appends the event. An agent a task that is
// not finished is assigned to cannot be removed.
func (s *Store) RemoveAgent(ctx context.Context, a domain.Agent, ev domain.Event) error {
	return s.Update(ctx, func(tx *Tx) error {
		var open int
		if err := tx.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE agent_id = ? AND state NOT IN ('completed', 'cancelled', 'failed')`, string(a.ID)).Scan(&open); err != nil {
			return fmt.Errorf("store: remove agent: %w", err)
		}
		if open > 0 {
			return domain.NewConflict(RuleInUse, "agent %s has %d unfinished task(s)", a.Role, open)
		}
		if _, err := tx.tx.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, string(a.ID)); err != nil {
			return fmt.Errorf("store: remove agent: %w", err)
		}
		_, err := tx.Append(ctx, ev)
		return err
	})
}

// LiveRuns lists the runs of every task that hold their environment: starting,
// running or paused. It is how the service applies the one-run-per-environment
// rule across tasks.
func (s *Store) LiveRuns(ctx context.Context, env domain.ID) ([]domain.Run, error) {
	return liveRuns(ctx, s.db, env)
}

// LiveRuns is Store.LiveRuns inside a transaction, so a check and the save that
// follows it cannot be separated by another writer.
func (tx *Tx) LiveRuns(ctx context.Context, env domain.ID) ([]domain.Run, error) {
	return liveRuns(ctx, tx.tx, env)
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func liveRuns(ctx context.Context, q querier, env domain.ID) ([]domain.Run, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, task_id, workspace_id, agent_id, env_id, state FROM runs WHERE env_id = ? AND state IN ('starting', 'running', 'paused') ORDER BY id`, string(env))
	if err != nil {
		return nil, fmt.Errorf("store: live runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Run
	for rows.Next() {
		var r domain.Run
		var id, task, ws, ag, e, st string
		if err := rows.Scan(&id, &task, &ws, &ag, &e, &st); err != nil {
			return nil, fmt.Errorf("store: live runs: %w", err)
		}
		r.ID, r.TaskID, r.WorkspaceID, r.AgentID, r.EnvID, r.State = domain.ID(id), domain.ID(task), domain.ID(ws), domain.ID(ag), domain.ID(e), domain.RunState(st)
		out = append(out, r)
	}
	return out, rows.Err()
}

// TaskSummary is one row of the task list.
type TaskSummary struct {
	ID        domain.ID
	Repo      string
	Issue     string
	State     domain.TaskState
	AgentID   domain.ID
	CreatedAt time.Time
}

// Tasks lists every task, newest first; onlyActive leaves out the finished ones.
func (s *Store) Tasks(ctx context.Context, onlyActive bool) ([]TaskSummary, error) {
	q := `SELECT id, repo, issue, state, agent_id, created_at FROM tasks`
	if onlyActive {
		q += ` WHERE state NOT IN ('completed', 'cancelled', 'failed')`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TaskSummary
	for rows.Next() {
		var t TaskSummary
		var id, state, agent string
		var created int64
		if err := rows.Scan(&id, &t.Repo, &t.Issue, &state, &agent, &created); err != nil {
			return nil, fmt.Errorf("store: tasks: %w", err)
		}
		t.ID, t.State, t.AgentID, t.CreatedAt = domain.ID(id), domain.TaskState(state), domain.ID(agent), fromNano(created)
		out = append(out, t)
	}
	return out, rows.Err()
}
