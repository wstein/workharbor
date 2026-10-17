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

## Host protocol

[Closed draft v1](host-protocol/README.md) and its
[structural schema](host-protocol/message.schema.json) define pure validation,
trusted-context and replay-disposition boundaries. No transport/provider/dispatcher
or durable ledger capability is implied.

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
BundleDock artifact after examining its upstream documentation and format. The
historical feature number collides with the Symphony smoke feature; allocate a
distinct identifier before planning bundle implementation.
[Workspace shell design input](../docs/papers/bundledock-workspace-shell.md) records
the proposed loading and prompt trust policy for issue #81; it does not change
these accepted ports or approve runtime integration.

## CLI contracts
- [`cli/exit-codes.json`](cli/exit-codes.json): frozen `whr` exit-code table, accepted in [ADR 0013](../docs/adr/0013-whr-exit-codes.md) (H15:b).
- [`cli/commands.json`](cli/commands.json): the single `whr` command table with group and short-form
  aliases, proposed in [ADR 0014](../docs/adr/0014-whr-command-structure.md) (H77). Design baseline;
  the Go table and its freeze tests are not implemented.
- [`cli/output.schema.json`](cli/output.schema.json): `--json` envelope and the results of the first-increment
  commands (`version`, `workspace.validate`, `workspace.init`, `host.doctor`); proposed, not implemented.
- [`cli/validate-findings.json`](cli/validate-findings.json): fully qualified `whr validate` finding codes and
  severities; proposed, not implemented.
- [`cli/doctor-checks.json`](cli/doctor-checks.json): the `whr doctor` check catalogue, thresholds and the
  first-increment boundary; proposed, not implemented.

## Workspace configuration manifest
[`workspace/workharbor.schema.json`](workspace/workharbor.schema.json) describes `workharbor.json`
(schema_version 1) as read today by the host agent resolver and `scripts/check_repo.py`; it adds no field.
`whr validate` and `whr init` (Feature 006) are specified to validate against it. The schema also describes the
proposed `schema_version` 2 (optional `name`, `start_dir`, `human_shell`, `console_mode`, `shortcuts`, `project`),
which the resolver rejects until a separate reviewed change. The Go validator is hand-written and frozen against the
shared vectors [`workspace/vectors.json`](workspace/vectors.json); product code uses no JSON Schema library (a
test-only validator may prove the output schema). [`workspace/starters.json`](workspace/starters.json) lists the
`whr workspace init` starters, all unsupported until provider admission;
[`cli/resources-vectors.json`](cli/resources-vectors.json) holds the shared CPU and memory parsing vectors (FR-059).
None of these is implemented.

## Agent session interface
[agent-session-v1.md](agent-session-v1.md) with its
[schema](agent-session-v1.schema.json): the versioned in-container session
interface (list, attach, status, send input, stop) of owner decision H16:b,
wstein/workharbor-nxt#71. Design baseline only; not implemented or verified.

## Selective Python migration

[migration/README.md](migration/README.md) records the pinned pure JSON/path
profiles, deliberate product restrictions and offline source/product vectors.
T003 implementation review is pending; no Go port or caller switch is claimed.
