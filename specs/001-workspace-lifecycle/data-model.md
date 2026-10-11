# Feature data model

See [domain contracts](../../contracts/README.md) for required fields and transitions.
Database foreign keys enforce workspace owner/host references. Unique operation key:
(owner_id, workspace_id, idempotency_key); retain canonical request hash and outcome.
User ownership does not depend on repository fork ownership. Session expiry and
atomic consumed_at update enforce single use. Storage references outlive instances.
Agent reports carry generation so stale reports cannot overwrite newer observations.
