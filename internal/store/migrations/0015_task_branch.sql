-- The integration branch a task started with (design D47, §6 "A task never runs
-- looser than the repository"): it publishes to that branch whatever the
-- configuration says later.
ALTER TABLE tasks ADD COLUMN integration_branch TEXT NOT NULL DEFAULT '';
