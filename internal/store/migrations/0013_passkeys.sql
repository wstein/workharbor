-- Passkeys (design D45, issue #101): the public key and the credential ID of each
-- enrolled authenticator, as the WebAuthn library serialises them, and nothing
-- secret. Enrolment and revocation are the host CLI's.
CREATE TABLE passkeys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    credential TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_used  INTEGER NOT NULL DEFAULT 0
);
