-- The agent's own tip a revision was prepared from, and whether it was pushed
-- (design §4.5): a follow-up round rebases only the agent's newer commits onto
-- the pushed revision, so pushed commits are never rewritten.
ALTER TABLE candidates ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE candidates ADD COLUMN pushed INTEGER NOT NULL DEFAULT 0;
