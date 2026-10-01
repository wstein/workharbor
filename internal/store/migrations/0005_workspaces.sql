-- Workspaces and agents (design D42, §4.3). They are registry records, not part
-- of a task's aggregate; their changes are audit events in the stream
-- "workspace:<id>". A workspace is not removed while it has agents, and a task
-- records the agent it is assigned to (empty for tasks made before agents).
CREATE TABLE workspaces (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    path        TEXT NOT NULL UNIQUE,
    repo        TEXT NOT NULL,
    integration TEXT NOT NULL,
    env_id      TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);

CREATE TABLE agents (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    role         TEXT NOT NULL,
    branch       TEXT NOT NULL,
    worktree     TEXT NOT NULL,
    instructions TEXT NOT NULL,
    profile      TEXT NOT NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    UNIQUE (workspace_id, role)
);

ALTER TABLE tasks ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
