CREATE TABLE initiations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('user_action', 'decision_answer')),
    actor TEXT NOT NULL,
    channel TEXT NOT NULL,
    at INTEGER NOT NULL,
    decision_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('agent.start', 'agent.resume', 'agent.say'))
);
