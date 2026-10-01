-- A task whose input came from an untrusted author (design §6, issue #53): the
-- policy decides with that context.
ALTER TABLE tasks ADD COLUMN untrusted INTEGER NOT NULL DEFAULT 0;
