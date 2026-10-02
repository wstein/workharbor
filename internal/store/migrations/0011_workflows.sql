-- Workflow presets (design D47, issue #105). A task keeps the preset its
-- repository had when it started, so a looser preset never applies to a run
-- already started; the preset a repository runs under is recorded, and each change
-- of it is an audit entry (policy changes are audited).
ALTER TABLE tasks ADD COLUMN workflow TEXT NOT NULL DEFAULT '';

CREATE TABLE repo_workflows (
    repo     TEXT PRIMARY KEY,
    workflow TEXT NOT NULL,
    since    INTEGER NOT NULL
);

CREATE TABLE workflow_changes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repo          TEXT NOT NULL,
    from_workflow TEXT NOT NULL,
    to_workflow   TEXT NOT NULL,
    confirmed_by  TEXT NOT NULL,
    at            INTEGER NOT NULL
);
