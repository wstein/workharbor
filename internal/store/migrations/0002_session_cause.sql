-- The agent's session ID is what survives a restart (design §5.3), and a
-- Decision records why a run is blocked on it and when a quota resets (§4.2).
-- A container's address is never stored: it changes on every start.
ALTER TABLE runs ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN cause TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN resume_at INTEGER NOT NULL DEFAULT 0;
