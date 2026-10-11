# Feature data model

See [domain contracts](../../contracts/README.md) for required fields and transitions.
Database foreign keys enforce workspace owner/host references. Unique operation key:
(owner_id, workspace_id, idempotency_key); retain canonical request hash and outcome.
User ownership does not depend on repository fork ownership. Session expiry and
atomic consumed_at update enforce single use. Storage references outlive instances.
Agent reports carry generation so stale reports cannot overwrite newer observations.

RevisionTrustApproval records owner_id, canonical repository identity, resolved
config commit, complete submodule path/commit set and approved_at. Approval matches
that exact revision set; any changed commit requires new approval. Provisioning
commands carry the matching approval reference and revisions.
Enrollment stores expires_at and atomic consumed_at; expiry is five minutes.
Session grants expire after 60 seconds; active relays enforce the spec's quotas,
maximum/idle lifetimes, dimensions and per-direction buffer limits. Host observations
become unknown after 30 seconds without authenticated heartbeat. Restore retains
identities, revocations and operation outcomes but resets observations to unknown.
