-- The size of a reviewed revision (issue #111): files changed and lines added and
-- removed against what it was prepared onto. The approval of "Ready to push?" copies
-- it into the review.approved audit entry, which the usage summary sums.
ALTER TABLE candidates ADD COLUMN files INTEGER NOT NULL DEFAULT 0;
ALTER TABLE candidates ADD COLUMN added INTEGER NOT NULL DEFAULT 0;
ALTER TABLE candidates ADD COLUMN removed INTEGER NOT NULL DEFAULT 0;
