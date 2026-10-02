-- A change to what a repository runs under that the configuration asks for and the
-- host has not confirmed (design D45, D47, issue #107). The supervisor keeps the
-- recorded workflow meanwhile; a passkey step-up on the web, or the host's
-- --accept-workflow-change, confirms it. Repository names are matched without
-- regard to case, as in repo_workflows.
CREATE TABLE pending_changes (
    id           TEXT PRIMARY KEY,
    repo         TEXT NOT NULL,
    from_workflow TEXT NOT NULL,
    from_branch  TEXT NOT NULL,
    to_workflow  TEXT NOT NULL,
    to_branch    TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('open', 'applied', 'withdrawn', 'stale')),
    created_at   INTEGER NOT NULL,
    closed_at    INTEGER NOT NULL DEFAULT 0,
    closed_by    TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX pending_changes_open ON pending_changes (repo) WHERE status = 'open';
