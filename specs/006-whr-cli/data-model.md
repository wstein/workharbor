# Feature 006 data model

Status: proposed extension of Feature 001 domain contracts.

| Entity | Fields and invariant |
| --- | --- |
| CLIProfile | canonical HTTPS endpoint, server identity, owner, credential reference/scope/expiry; private atomic files, no workspace/Git secret. |
| OwnerCredential | owner, opaque token hash, scopes, expiry/revocation; dashboard cannot mutate or mint mutation grants. |
| Submission | preallocated owner/workspace/key, canonical request hash and operation; changed payload conflicts before provider calls. |
| ConsoleSession | workspace, runtime generation, start time, process/PTY, state, attachments, 64 KiB byte ring and dropped count; one active/workspace. |
| Attachment | owner/workspace/session/audience, consumed grant, monotonic lifetimes, quotas/buffers; independent of session lifetime. |
| Grant | issuer/key ID, owner, workspace/generation, audience, kind/operations, optional session, jti/expiry; attach atomically consumed across endpoint restart. |
| HostCheckpoint | host/operation, completed/remaining steps, confirmed revocation; private, atomic, no embedded secrets, incomplete never success. |
| ArtifactProjection | repository/feature/task, commit/paths/content hashes, parser capability, evidence source/time and availability; checkbox never approval. |
| Envelope/Event | schema version, typed data or redacted error, operation/key where known, CLI/remote exit kind; base64 stream bytes. |
| CommandTable | canonical id, group path, optional short form, group aliases, increment, dependency, writes, `--json` mode; frozen against `contracts/cli/commands.json`; a short form is an alias of one command, never a copy. |
| ManifestV2 | proposed schema_version 2 of workharbor.json: the four version 1 fields plus optional name, start_dir, human_shell, console_mode, shortcuts, project (url, path); defaults fish, plain, on, '.' (Feature 010 OQ-7; P3 merged in #222); fail-closed until the resolver accepts it. |
| ResourceSelection | CPU integer thousandths and memory integer bytes from the pure library; per-field origin (flag, host_default, missing); no wiring in the first increment. |
| ValidationResult | directory as supplied, valid, counts, at most 200 findings (code, severity, subject, message, remediation), truncated, `provider_admission` always `not_evaluated`; never contains file content or a credential. |
| InitProposal | ordered file actions (create, overwrite, reuse, skip_existing, append_block, replace_block, left_unchanged) with content hash and diff, the effective manifest settings with origin (flag, existing, default), dry-run flag, created and overwritten path list with device and inode for rollback, validation result; no credential. |
| DoctorCheck | catalogue id, status pass/fail/skipped/unknown, reason when skipped or unknown, redacted evidence, remediation when failed; verdict ok/failed/incomplete. |
| PlatformEvidence | exact source, command, client/host versions, observed results/date; a build target is not verified platform support. |

Retain Feature 001 ownership, separate storage lifecycle, desired/observed state,
generation checks and operation outcomes. Restore resets observations to unknown
and does not start runtime. Lost agent-input delivery is unknown, never auto-resend.

## Accepted T005 persistence foundation

[Native identity and guards](contracts/owner-api.md#accepted-t005-foundation-contract)
are authoritative. SQLite foreign keys enforce owner/host/workspace scope;
changesets alone are insufficient. Hosts have mandatory owner_id and cannot be
reassigned in the pilot. Workspace host/owner, operation workspace/owner, and
storage/runtime workspace/host references are composite pairs. Binding records
pair operation/workspace and runtime/workspace. Use pairwise references because
the locked adapter supports only two-column total composite FK helpers.

Operations enforce unique (owner, workspace, key), unique (workspace, intent_version)
and one queued/executing operation/workspace. Runtime generations are unique per
workspace; a reservation belongs to one operation with at most one candidate.
One runtime may be marked current; pending candidates are not current. Opaque
provider handles are nullable until allocation and runtime IDs immutable once set.

Workspace intent_version starts at zero and advances once/new accepted operation.
last_runtime_generation is a separate allocator: only a new RuntimeInstance
reservation advances it. Pending reservation is not a running/current replacement.
Replay allocates nothing; abandoned generations are never reused. Start/stop of
the same target keep runtime generation. Results bind operation+intent version
and runtime ID+generation, preventing old same-runtime results from overwriting
new intent. Desired states remain running/stopped, not deleted.

Requested configuration is immutable in operation identity. Workspace requested
config is only desired projection. Runtime effective_config remains null until
validated observation, and never changes on an older runtime just because rebuild
was queued/failed. Candidate config comes solely from its requesting operation.
T005 start requires an existing current runtime with nonnull effective config,
captures that immutable execution_config_snapshot, and cannot reserve replacement;
reviewed replacement-start behavior remains T010. Failed rebuild workspace display
and session invalidation are later lifecycle work, not metadata success claims.

Storage outlives runtime. Runtime removal preserves workspace/storage, and only
confirmed outcomes set tombstones. Retain IDs and operation key/outcome history
indefinitely for internal pilot. Data deletion remains unsupported in T005.

Explicit file-backed WAL, foreign_keys=on, synchronous=full, busy_timeout=2000 ms
are required. Verify FK/synchronous on every writer connection. Race/crash tests
use independently committed connections outside wrapping SQL Sandbox transactions:
Exqlite turns nested immediate transactions into SAVEPOINT. Reopen/process-kill
results do not establish hardware power-loss resilience.
