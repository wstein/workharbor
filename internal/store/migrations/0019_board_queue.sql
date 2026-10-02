-- The cards of the board's agent queue the supervisor has already seen (design D30,
-- D40, issue #71): when each was last changed and the task it raised. A card raises
-- its "Accept this task?" Decision once per state, so declining it does not ask
-- again until the card is moved again. Repository names are matched without regard
-- to case, as in repo_workflows.
CREATE TABLE board_queue (
    repo       TEXT NOT NULL,
    issue      INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    task_id    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (repo, issue)
);
