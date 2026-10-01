-- Tasks are saved with their runs, environments and review candidates, and
-- guarded by one version (compare-and-swap, design §5.4).
CREATE TABLE tasks (
    id         TEXT PRIMARY KEY,
    version    INTEGER NOT NULL,
    repo       TEXT NOT NULL,
    issue      TEXT NOT NULL,
    state      TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE environments (
    id      TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    backend TEXT NOT NULL,
    state   TEXT NOT NULL
);

CREATE TABLE runs (
    id           TEXT PRIMARY KEY,
    task_id      TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    env_id       TEXT NOT NULL,
    state        TEXT NOT NULL,
    ord          INTEGER NOT NULL
);

CREATE TABLE candidates (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sha     TEXT NOT NULL,
    run_id  TEXT NOT NULL,
    branch  TEXT NOT NULL,
    pr_url  TEXT NOT NULL,
    ci      TEXT NOT NULL,
    ord     INTEGER NOT NULL,
    PRIMARY KEY (task_id, sha)
);

-- A Decision is its own aggregate with its own version, so an answer and an
-- expiry of one Decision cannot both win.
CREATE TABLE decisions (
    id              TEXT PRIMARY KEY,
    version         INTEGER NOT NULL,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    run_id          TEXT NOT NULL,
    kind            TEXT NOT NULL,
    blocking        INTEGER NOT NULL,
    subject         TEXT NOT NULL,
    input           TEXT NOT NULL,
    input_truncated INTEGER NOT NULL,
    sha             TEXT NOT NULL,
    options         TEXT NOT NULL,
    status          TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    timeout_ns      INTEGER NOT NULL,
    deadline        INTEGER NOT NULL,
    answered_at     INTEGER NOT NULL,
    answer          TEXT NOT NULL,
    reason          TEXT NOT NULL,
    answered_by     TEXT NOT NULL,
    superseded_by   TEXT NOT NULL
);
CREATE INDEX decisions_task ON decisions (task_id, created_at);

-- The event log. seq is monotonic and never reused, which is what --since
-- replays. Audit rows can never be changed or deleted; transcript rows can
-- only be deleted by Purge, which records itself.
CREATE TABLE events (
    seq     INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    kind    TEXT NOT NULL,
    tier    TEXT NOT NULL CHECK (tier IN ('audit', 'transcript')),
    payload BLOB NOT NULL,
    at      INTEGER NOT NULL
);
CREATE INDEX events_task ON events (task_id, seq);

CREATE TRIGGER events_no_update BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE TRIGGER events_audit_no_delete BEFORE DELETE ON events
WHEN OLD.tier = 'audit'
BEGIN
    SELECT RAISE(ABORT, 'audit events are never deleted');
END;

-- A mutating command runs under its key; the response is stored with the
-- changes, so a replay returns it without doing anything again.
CREATE TABLE idempotency (
    key          TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    response     BLOB NOT NULL,
    created_at   INTEGER NOT NULL
);
