-- Launches of the agent that failed since the run last ran (design §5.3): the
-- reconciler fails a run when its attempts are used up.
ALTER TABLE runs ADD COLUMN resume_attempts INTEGER NOT NULL DEFAULT 0;
