-- An environment now serves every task of its workspace (design D42), one after
-- the other, so the same environment ID appears in several tasks. The key is
-- the pair.
CREATE TABLE environments_new (
    id      TEXT NOT NULL,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    backend TEXT NOT NULL,
    state   TEXT NOT NULL,
    PRIMARY KEY (task_id, id)
);
INSERT INTO environments_new (id, task_id, backend, state) SELECT id, task_id, backend, state FROM environments;
DROP TABLE environments;
ALTER TABLE environments_new RENAME TO environments;
