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
| PlatformEvidence | exact source, command, client/host versions, observed results/date; a build target is not verified platform support. |

Retain Feature 001 ownership, separate storage lifecycle, desired/observed state,
generation checks and operation outcomes. Restore resets observations to unknown
and does not start runtime. Lost agent-input delivery is unknown, never auto-resend.
