-- The manifest digest a feature reference resolved to when its Decision was raised,
-- and the digest an allow was given for (design D38, issue #127): a tag can be moved,
-- so an allow applies only while the reference still resolves to the digest the human
-- saw. Empty for every other Decision, and for an answer given before this column
-- existed, which therefore never matches and is asked again.
ALTER TABLE decisions ADD COLUMN feature_digest TEXT NOT NULL DEFAULT ''
    CHECK (feature_digest = '' OR (length(feature_digest) = 71 AND substr(feature_digest, 1, 7) = 'sha256:'));
ALTER TABLE feature_sources ADD COLUMN digest TEXT NOT NULL DEFAULT ''
    CHECK (digest = '' OR (length(digest) = 71 AND substr(digest, 1, 7) = 'sha256:'));
