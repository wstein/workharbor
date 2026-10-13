# Domain contracts v1 (design baseline)

These are reviewable boundary contracts, not a live API. Unknown protocol versions
fail negotiation. Runtime transport serialization and validation are implemented
in feature 001; no unvalidated JSON reaches provider operations.

| Entity | Required identity and state |
| --- | --- |
| User | id, display_name; one local owner initially |
| Workspace | id, owner_id, host_id, config repository URL + commit, desired state |
| RuntimeInstance | id, workspace_id, provider_instance_id, observed state, generation |
| Host | id, agent identity fingerprint, capabilities, last_seen, lease expiry |
| Operation | id, idempotency_key, owner_id, workspace_id, request hash, status, timestamps |
| PersistentStorage | id, workspace_id, host_id, provider handle; independent lifecycle |
| BundleAttachment | workspace_id, artifact digest, platform, method, resolved paths |
| Session | id, workspace_id, owner_id, expires_at, consumed_at; no reusable token in Git |

## Lifecycle and idempotency
Desired states: running, stopped. Observed states: pending, provisioning, running,
stopping, stopped, failed, unknown. Unknown is required on stale/disconnected host.
Operation status: queued, executing, succeeded, failed. Reusing an idempotency key
with the same normalized request returns the recorded outcome; with a different
request it returns a conflict. A unique constraint is scoped to owner and workspace.
Stop is safe to retry; start preserves volume; provisioning recovers after restart.
Destructive storage deletion is a distinct authorized operation, outside feature 001.

## Provider port
`capabilities()`, `validate(config)`, `provision(resolved, operation)`, `start(handle,
operation)`, `stop(handle, operation)`, `inspect(handle)`, `open_pty(handle, grant)`.
Handles are opaque outside adapters. Report errors with stable codes and redacted
human explanations. Deadlines and cancellation apply to every mutation.

## Bundle ports
`BundleResolver.resolve(reference, target_platform)` returns immutable digest,
verified integrity, dependencies, tool paths and attachment requirements.
`BundleAttacher.validate(resolved, capabilities)` precedes provisioning;
`attach(resolved, runtime, operation)` returns a recorded attachment. Removal must
not alter the language image or delete project data. Feature 002 tests one real
Bundledock artifact after examining its upstream documentation and format.

## CLI contracts
- [`cli/exit-codes.json`](cli/exit-codes.json): frozen `whr` exit-code table, proposed in [ADR 0013](../docs/adr/0013-whr-exit-codes.md) (H15:b).
