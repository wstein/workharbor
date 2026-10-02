-- The integration branch of a repository's workflow is recorded next to its preset
-- (design D47, §6): changing it is a policy change like changing the preset, refused
-- at start until the host confirms it and recorded in workflow_changes. Repository
-- names are matched without regard to case, so a name written in another case is the
-- same repository and cannot slip past the check.
ALTER TABLE repo_workflows ADD COLUMN branch TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_changes ADD COLUMN from_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_changes ADD COLUMN to_branch TEXT NOT NULL DEFAULT '';
UPDATE OR IGNORE repo_workflows SET repo = lower(repo);
UPDATE workflow_changes SET repo = lower(repo);
