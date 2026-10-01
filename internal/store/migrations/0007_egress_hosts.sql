-- The human's answers to egress requests, per repository (design D38). A
-- repository's devcontainer.json may request an egress host and its lockfiles
-- suggest registries; neither allows anything. Only an answer here does, and it
-- lives on the supervisor's side, never in the repository.
CREATE TABLE egress_hosts (
    repo        TEXT NOT NULL,
    hostname    TEXT NOT NULL,
    allowed     INTEGER NOT NULL CHECK (allowed IN (0, 1)),
    decision_id TEXT NOT NULL DEFAULT '',
    answered_at INTEGER NOT NULL,
    PRIMARY KEY (repo, hostname)
);
