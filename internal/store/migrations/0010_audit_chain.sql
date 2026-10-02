-- A tamper-evident chain over the audit entries (design §7.7). Each audit event
-- gets one row here, written in the same transaction: a hash of the event and of
-- the hash before it, so changing, removing or inserting an entry breaks every
-- hash after it. The commit SHA an entry names, if it names one, is kept here
-- too, so the audit trail of a commit can be read by SHA. Rows are append-only,
-- like the events they cover. Audit entries from before this migration have no
-- row and are not covered.
CREATE TABLE audit_chain (
    seq  INTEGER PRIMARY KEY,
    prev TEXT NOT NULL,
    hash TEXT NOT NULL,
    sha  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_chain_sha ON audit_chain (sha) WHERE sha != '';

CREATE TRIGGER audit_chain_no_update BEFORE UPDATE ON audit_chain
BEGIN
    SELECT RAISE(ABORT, 'the audit chain is append-only');
END;

CREATE TRIGGER audit_chain_no_delete BEFORE DELETE ON audit_chain
BEGIN
    SELECT RAISE(ABORT, 'the audit chain is never deleted');
END;
