-- What an egress answer was given for (design D47). For a host a devcontainer.json
-- requested, a digest of that file at the commit the environment was built from;
-- "lockfile" for one a lockfile suggested; "unknown" for an answer from before this
-- column. A repository under the published preset asks again when it differs.
ALTER TABLE egress_hosts ADD COLUMN source TEXT NOT NULL DEFAULT 'unknown';
