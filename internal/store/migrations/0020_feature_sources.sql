-- The feature source a Decision is about (design §4.2, D38, issue #127): the
-- reference as the repository wrote it, validated by the reader and set only for a
-- Decision with the cause feature_source.
ALTER TABLE decisions ADD COLUMN feature TEXT NOT NULL DEFAULT '';

-- The human's answers about devcontainer feature sources outside the allowed one,
-- per repository and reference. Only an answer here lets a feature from such a
-- source be fetched; it lives on the supervisor's side, never in the repository.
CREATE TABLE feature_sources (
    repo        TEXT NOT NULL,
    ref         TEXT NOT NULL,
    allowed     INTEGER NOT NULL CHECK (allowed IN (0, 1)),
    decision_id TEXT NOT NULL DEFAULT '',
    answered_at INTEGER NOT NULL,
    PRIMARY KEY (repo, ref)
);
