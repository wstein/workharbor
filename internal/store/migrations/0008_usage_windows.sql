-- The latest reading of each usage window of an account (design §5.7, D40).
-- Concurrent runs on one subscription share its windows, so a window is one
-- figure per account and name, not per task. The per-turn usage itself is an
-- audit entry in the event log ("usage.recorded") and is never purged.
CREATE TABLE usage_windows (
    account     TEXT NOT NULL,
    name        TEXT NOT NULL,
    utilization REAL NOT NULL CHECK (utilization >= 0 AND utilization <= 1),
    resets_at   INTEGER NOT NULL DEFAULT 0,
    at          INTEGER NOT NULL,
    PRIMARY KEY (account, name)
);
